const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '..', 'Code.gs'), 'utf8');

function script() {
  const properties = new Map([
    ['API_URL', 'https://cashcraft.example.org'],
    ['ACCESS_TOKEN', 'test-token'],
    ['EXPIRES_AT', String(Math.floor(Date.now() / 1000) + 3600)]
  ]);
  const calls = [];
  const context = vm.createContext({
    Date, console,
    Utilities: {formatDate: (date) => date.toISOString().slice(0, 10)},
    PropertiesService: {getUserProperties: () => ({
      getProperty: (key) => properties.get(key) ?? null,
      deleteProperty: (key) => properties.delete(key),
      setProperties: (items) => Object.entries(items).forEach(([key, value]) => properties.set(key, value))
    })},
    UrlFetchApp: {fetch: (url, options) => {
      calls.push({url, options});
      return {getResponseCode: () => 200, getContentText: () => '{"id":42}'};
    }},
    LockService: {getDocumentLock: () => ({tryLock: () => true, releaseLock: () => {}})},
    Session: {getEffectiveUser: () => ({getEmail: () => 'owner@example.org'})}
  });
  vm.runInContext(source, context);
  return {context, properties, calls};
}

test('API URL requires an HTTPS origin, without credentials or /api path', () => {
  const {context} = script();
  assert.equal(context.validateAPIURL_(' https://cashcraft.example.org/ '), 'https://cashcraft.example.org');
  for (const url of ['http://example.org', 'https://user:pass@example.org', 'https://example.org/api', 'https://example.org?token=secret']) {
    assert.throws(() => context.validateAPIURL_(url));
  }
});

test('transaction row validates dates, required fields and numeric spreadsheet cells', () => {
  const {context} = script();
  const tx = context.transactionFromRow_(['2026-11-05', 450, ' еда ', 'Обед'], 'Europe/Moscow');
  assert.equal(JSON.stringify(tx), JSON.stringify({date: '2026-11-05', amount: 450, category: 'еда', description: 'Обед'}));
  for (const date of ['2026-02-29', '2026-13-01', '05.11.2026', '']) {
    assert.throws(() => context.dateString_(date, 'Europe/Moscow'));
  }
  for (const amount of [0, -1, '450,00', Infinity, NaN]) {
    assert.throws(() => context.transactionFromRow_(['2026-11-05', amount, 'еда', ''], 'Europe/Moscow'));
  }
  assert.throws(() => context.category_('   '));
});

test('requests pass a JWT and do not follow redirects', () => {
  const {context, calls} = script();
  const result = context.requestAPI_('/api/transactions', 'post', {amount: 450});
  assert.equal(result.id, 42);
  assert.equal(calls[0].url, 'https://cashcraft.example.org/api/transactions');
  assert.equal(calls[0].options.headers.Authorization, 'Bearer test-token');
  assert.equal(calls[0].options.followRedirects, false);
  assert.equal(calls[0].options.payload, '{"amount":450}');
});

test('expired sessions are cleared without sending a request', () => {
  const {context, properties, calls} = script();
  properties.set('EXPIRES_AT', '1');
  assert.throws(() => context.requestAPI_('/api/budgets', 'get'), /Войдите/);
  assert.equal(properties.has('ACCESS_TOKEN'), false);
  assert.equal(calls.length, 0);
});

test('HTTP validation errors and expired authorization are surfaced', () => {
  const {context, properties} = script();
  context.UrlFetchApp.fetch = () => ({getResponseCode: () => 409, getContentText: () => '{"error":"budget exceeded"}'});
  assert.throws(() => context.requestAPI_('/api/transactions', 'post', {}), /409: budget exceeded/);
  context.UrlFetchApp.fetch = () => ({getResponseCode: () => 401, getContentText: () => '{"error":"unauthorized"}'});
  assert.throws(() => context.requestAPI_('/api/budgets', 'get'), /авторизации/);
  assert.equal(properties.has('ACCESS_TOKEN'), false);
});

test('passwords are only sent to authentication and never stored', () => {
  const {context, properties, calls} = script();
  const fetch = context.UrlFetchApp.fetch;
  context.UrlFetchApp.fetch = (url, options) => {
    fetch(url, options);
    return {getResponseCode: () => 201, getContentText: () => '{"access_token":"fresh-token","expires_at":9999999999}'};
  };
  context.authenticate('register', ' person@example.org ', 'correct-password');
  assert.equal(calls[0].options.headers.Authorization, undefined);
  assert.equal(JSON.parse(calls[0].options.payload).password, 'correct-password');
  assert.equal(properties.get('ACCESS_TOKEN'), 'fresh-token');
  assert.equal([...properties.values()].includes('correct-password'), false);
});

function rowSheet(values) {
  const writes = [];
  return {
    writes,
    getParent: () => ({getSpreadsheetTimeZone: () => 'Europe/Moscow'}),
    getRange: (row, column) => ({
      getValues: () => [values],
      setValue: (value) => {writes.push({row, column, value}); return this;}
    })
  };
}

test('successful submission stores the ID; a sent transaction is never submitted again', () => {
  const {context, calls} = script();
  const sheet = rowSheet(['2026-11-05', 450, 'еда', 'Обед', true, '', '']);
  context.sendTransactionRow_(sheet, 2);
  assert.ok(sheet.writes.some(({column, value}) => column === 6 && value === 42));
  assert.ok(sheet.writes.some(({column, value}) => column === 5 && value === false));
  context.sendTransactionRow_(rowSheet(['2026-11-05', 450, 'еда', 'Обед', true, 42, 'Сохранено']), 2);
  assert.equal(calls.length, 1);
});

test('failed transaction leaves ID empty and puts the error in the status column', () => {
  const {context} = script();
  context.UrlFetchApp.fetch = () => ({getResponseCode: () => 409, getContentText: () => '{"error":"budget exceeded"}'});
  const sheet = rowSheet(['2026-11-05', 5000, 'еда', '', true, '', '']);
  assert.throws(() => context.sendTransactionRow_(sheet, 2), /budget exceeded/);
  assert.equal(sheet.writes.some(({column}) => column === 6), false);
  assert.ok(sheet.writes.some(({column, value}) => column === 7 && value.includes('budget exceeded')));
});

test('automatic submission never uses the trigger owner token for another editor', () => {
  const {context, calls} = script();
  const sheet = rowSheet([]);
  sheet.getName = () => 'Транзакции';
  let checked = true;
  const event = {
    value: 'TRUE', user: {getEmail: () => 'other@example.org'},
    range: {getNumRows: () => 1, getNumColumns: () => 1, getRow: () => 2, getColumn: () => 5,
      getSheet: () => sheet, setValue: (value) => {checked = value;}}
  };
  context.cashcraftOnEdit(event);
  assert.equal(calls.length, 0);
  assert.equal(checked, false);
});

test('report categories cannot become spreadsheet formulas', () => {
  const {context} = script();
  assert.equal(context.safeCellText_('=IMPORTXML("https://example.org")'), '\'=IMPORTXML("https://example.org")');
  assert.equal(context.safeCellText_('еда'), 'еда');
});

test('report loads API results into cells and formats percent values', () => {
  const {context, calls} = script();
  const writes = [];
  const sheet = {
    getLastRow: () => 10,
    getMaxRows: () => 1000,
    getRange: (...coordinates) => {
      const range = {
        getValues: () => [['2026-11-01'], ['2026-11-30']],
        setValue: (value) => {writes.push({coordinates, value}); return range;},
        setValues: (value) => {writes.push({coordinates, value}); return range;},
        setNumberFormat: (value) => {writes.push({coordinates, format: value}); return range;},
        clearContent: () => {writes.push({coordinates, cleared: true}); return range;}
      };
      return range;
    }
  };
  context.SpreadsheetApp = {getActiveSpreadsheet: () => ({
    getSheetByName: () => sheet, getSpreadsheetTimeZone: () => 'Europe/Moscow'
  })};
  const fetch = context.UrlFetchApp.fetch;
  context.UrlFetchApp.fetch = (url, options) => {
    fetch(url, options);
    return {getResponseCode: () => 200, getContentText: () => JSON.stringify({
      total: 600,
      categories: [
        {category: 'еда', total: 450, has_budget: true, budget_limit: 5000, budget_used_percent: 9},
        {category: '=1+1', total: 150, has_budget: false, budget_limit: 0, budget_used_percent: 0}
      ]
    })};
  };
  context.getReport();
  assert.equal(calls[0].url, 'https://cashcraft.example.org/api/reports/summary?from=2026-11-01&to=2026-11-30');
  assert.ok(writes.some(({coordinates, value}) => coordinates[0] === 'B5' && value === 600));
  const rows = writes.find(({coordinates, value}) => coordinates[0] === 8 && Array.isArray(value)).value;
  assert.equal(JSON.stringify(rows), JSON.stringify([['еда', 450, 5000, 0.09], ["'=1+1", 150, '', '']]));
  assert.ok(writes.some(({cleared}) => cleared));
});

test('setup rejects an unrelated sheet with the same name without changing it', () => {
  const {context} = script();
  const headers = ['Existing data'];
  const sheet = {getLastRow: () => 5, getRange: () => ({getValues: () => [headers]})};
  const ss = {getSheetByName: () => sheet};
  assert.throws(() => context.setupTable_(ss, 'Транзакции', ['Дата', 'Сумма'], 5), /уже содержит/);
});

test('setup restores XLSX checkbox controls and formatting without replacing imported data', () => {
  for (const fixture of [
    {
      name: 'Транзакции', checkbox: 5,
      rows: [
        ['Дата', 'Сумма', 'Категория', 'Описание', 'Отправить', 'ID', 'Статус'],
        ['2026-11-05', 450, 'еда', 'Обед', false, 42, 'Сохранено'],
        ['2026-11-05', 150, 'транспорт', 'Поездка', true, '', ''],
        ['2026-11-05', 100, 'еда', '', '', '', '']
      ]
    },
    {
      name: 'Бюджеты', checkbox: 3,
      rows: [
        ['Категория', 'Лимит', 'Отправить', 'Статус'],
        ['еда', 5000, true, ''], ['транспорт', 2000, false, 'Сохранено']
      ]
    }
  ]) {
    const {context} = script();
    const original = JSON.stringify(fixture.rows);
    const operations = [];
    const rule = {checkbox: true};
    context.SpreadsheetApp = {newDataValidation: () => ({requireCheckbox: () => ({build: () => rule})})};
    const sheet = {
      getLastRow: () => fixture.rows.length,
      getMaxRows: () => 100,
      setFrozenRows: (count) => operations.push({frozenRows: count}),
      setColumnWidths: (...args) => operations.push({widths: args}),
      getRange: (...coordinates) => {
        const range = {
          getValues: () => [fixture.rows[0]],
          setValues: () => {throw new Error('Existing data must not be rewritten');},
          insertCheckboxes: () => {throw new Error('insertCheckboxes resets existing checkbox values');},
          setFontWeight: () => range,
          setBackground: () => range,
          setDataValidation: (validation) => {operations.push({coordinates, validation}); return range;},
          setNumberFormat: (format) => {operations.push({coordinates, format}); return range;}
        };
        return range;
      }
    };
    const ss = {getSheetByName: () => sheet};
    context.setupTable_(ss, fixture.name, fixture.rows[0], fixture.checkbox);
    assert.equal(JSON.stringify(fixture.rows), original);
    const checkbox = operations.find(({validation}) => validation === rule);
    assert.deepEqual(checkbox.coordinates, [2, fixture.checkbox, 99, 1]);
    assert.ok(operations.some(({frozenRows}) => frozenRows === 1));
    assert.ok(operations.some(({format}) => format === '0.00'));
    assert.equal(operations.some(({format}) => format === 'yyyy-mm-dd'), fixture.name === 'Транзакции');
  }
});

test('document lock is always released after a failing API operation', () => {
  const {context} = script();
  let released = false;
  context.LockService.getDocumentLock = () => ({tryLock: () => true, releaseLock: () => {released = true;}});
  assert.throws(() => context.withDocumentLock_(() => {throw new Error('failure');}), /failure/);
  assert.equal(released, true);
});
