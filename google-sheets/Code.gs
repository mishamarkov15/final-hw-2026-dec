const CASHCRAFT = {
  transactions: 'Транзакции',
  budgets: 'Бюджеты',
  report: 'Отчёт',
  transactionHeaders: ['Дата', 'Сумма', 'Категория', 'Описание', 'Отправить', 'ID', 'Статус'],
  budgetHeaders: ['Категория', 'Лимит', 'Отправить', 'Статус']
};

function onOpen() {
  SpreadsheetApp.getUi().createMenu('CashCraft')
    .addItem('Подготовить листы', 'setupSheets')
    .addItem('Адрес API', 'configureAPI')
    .addItem('Регистрация / вход', 'showAuthDialog')
    .addSeparator()
    .addItem('Отправить выбранную транзакцию', 'sendSelectedTransaction')
    .addItem('Сохранить выбранный бюджет', 'saveSelectedBudget')
    .addItem('Получить отчёт', 'getReport')
    .addItem('Включить отправку по флажку', 'installEditTrigger')
    .addSeparator()
    .addItem('Выйти', 'logout')
    .addToUi();
}

function setupSheets() {
  setupSheets_(SpreadsheetApp.getActiveSpreadsheet());
  onOpen();
}

function setupSheets_(ss) {
  setupTable_(ss, CASHCRAFT.transactions, CASHCRAFT.transactionHeaders, 5);
  setupTable_(ss, CASHCRAFT.budgets, CASHCRAFT.budgetHeaders, 3);
  let sheet = ss.getSheetByName(CASHCRAFT.report);
  if (!sheet) sheet = ss.insertSheet(CASHCRAFT.report);
  if (sheet.getLastRow() !== 0) return;
  const today = Utilities.formatDate(new Date(), ss.getSpreadsheetTimeZone(), 'yyyy-MM-dd');
  sheet.getRange('A1').setValue('CashCraft — отчёт о расходах');
  sheet.getRange('A2:A3').setValues([['С даты'], ['По дату']]);
  sheet.getRange('B2:B3').setNumberFormat('@').setValues([[today.slice(0, 8) + '01'], [today]]);
  sheet.getRange('A5:B5').setValues([['Всего расходов', 0]]);
  sheet.getRange('A7:D7').setValues([['Категория', 'Расходы за период', 'Бюджет', 'Использовано за период']]);
  sheet.getRange('A7:D7').setFontWeight('bold').setBackground('#d9ead3');
  sheet.setColumnWidths(1, 4, 180);
}

function setupTable_(ss, name, headers, checkboxColumn) {
  let sheet = ss.getSheetByName(name);
  if (!sheet) sheet = ss.insertSheet(name);
  if (sheet.getLastRow() !== 0) {
    const current = sheet.getRange(1, 1, 1, headers.length).getValues()[0];
    if (current.join('\t') !== headers.join('\t')) {
      throw new Error('Лист «' + name + '» уже содержит другие данные. Переименуйте его перед настройкой.');
    }
  } else {
    sheet.getRange(1, 1, 1, headers.length).setValues([headers]);
  }
  sheet.getRange(1, 1, 1, headers.length).setFontWeight('bold').setBackground('#d9ead3');
  sheet.setFrozenRows(1);
  sheet.setColumnWidths(1, headers.length, 160);
  if (sheet.getMaxRows() < 2) sheet.insertRowsAfter(1, 1);
  const checkboxRule = SpreadsheetApp.newDataValidation().requireCheckbox().build();
  sheet.getRange(2, checkboxColumn, sheet.getMaxRows() - 1, 1).setDataValidation(checkboxRule);
  sheet.getRange(2, 2, sheet.getMaxRows() - 1, 1).setNumberFormat('0.00');
  if (name === CASHCRAFT.transactions) {
    sheet.getRange(2, 1, sheet.getMaxRows() - 1, 1).setNumberFormat('yyyy-mm-dd');
  }
}

function createDemoSpreadsheet() {
  const ss = SpreadsheetApp.create('CashCraft — личные расходы');
  setupSheets_(ss);
  const today = Utilities.formatDate(new Date(), ss.getSpreadsheetTimeZone(), 'yyyy-MM-dd');
  ss.getSheetByName(CASHCRAFT.transactions).getRange('A2:D3').setValues([
    [today, 450, 'еда', 'Обед'], [today, 150, 'транспорт', 'Поездка']
  ]);
  ss.getSheetByName(CASHCRAFT.budgets).getRange('A2:B3').setValues([['еда', 5000], ['транспорт', 2000]]);
  console.log(ss.getUrl());
  return ss.getUrl();
}

function configureAPI() {
  const ui = SpreadsheetApp.getUi();
  const result = ui.prompt('Адрес API', 'Публичный HTTPS-адрес Gateway без /api, например https://cashcraft.example.org', ui.ButtonSet.OK_CANCEL);
  if (result.getSelectedButton() !== ui.Button.OK) return;
  const url = validateAPIURL_(result.getResponseText());
  const properties = PropertiesService.getUserProperties();
  if (properties.getProperty('API_URL') !== url) clearSession_();
  properties.setProperty('API_URL', url);
  ui.alert('Адрес API сохранён. Выполните вход через меню CashCraft.');
}

function validateAPIURL_(value) {
  const url = String(value).trim().replace(/\/+$/, '');
  if (!/^https:\/\/[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]+)?$/i.test(url)) {
    throw new Error('Укажите HTTPS-адрес без пути, логина, параметров и /api.');
  }
  return url;
}

function showAuthDialog() {
  const html = HtmlService.createHtmlOutput(`<!doctype html><html><head><base target="_top"><style>
    body{font:14px Arial;padding:16px}label{display:block;margin-top:12px}input,select{box-sizing:border-box;width:100%;padding:8px}button{margin-top:16px;padding:10px 16px}#result{color:#a00;white-space:pre-wrap}
    </style></head><body><form id="auth"><label>Действие<select id="mode"><option value="login">Вход</option><option value="register">Регистрация</option></select></label>
    <label>Email<input id="email" type="email" autocomplete="username" required></label>
    <label>Пароль<input id="password" type="password" autocomplete="current-password" minlength="8" required></label>
    <p>При регистрации: пароль от 8 до 72 байт.</p><button id="submit" type="submit">Продолжить</button><p id="result" role="status"></p></form>
    <script>document.getElementById('auth').addEventListener('submit',function(e){e.preventDefault();var button=document.getElementById('submit');button.disabled=true;document.getElementById('result').textContent='';
    google.script.run.withSuccessHandler(function(){document.getElementById('password').value='';google.script.host.close();}).withFailureHandler(function(err){document.getElementById('password').value='';document.getElementById('result').textContent=err.message;button.disabled=false;}).authenticate(document.getElementById('mode').value,document.getElementById('email').value,document.getElementById('password').value);});</script></body></html>`).setWidth(400).setHeight(420);
  SpreadsheetApp.getUi().showModalDialog(html, 'CashCraft — авторизация');
}

function authenticate(mode, email, password) {
  if (mode !== 'login' && mode !== 'register') throw new Error('Неизвестное действие авторизации.');
  const response = requestAPI_('/api/auth/' + mode, 'post', {email: String(email).trim(), password: password}, false);
  if (!response.access_token || !Number.isFinite(response.expires_at)) throw new Error('API вернул некорректный ответ авторизации.');
  PropertiesService.getUserProperties().setProperties({
    ACCESS_TOKEN: response.access_token,
    EXPIRES_AT: String(response.expires_at)
  });
  return 'Вход выполнен';
}

function logout() {
  clearSession_();
  SpreadsheetApp.getUi().alert('Вы вышли из CashCraft.');
}

function clearSession_() {
  const properties = PropertiesService.getUserProperties();
  properties.deleteProperty('ACCESS_TOKEN');
  properties.deleteProperty('EXPIRES_AT');
}

function requestAPI_(path, method, payload, authorized) {
  const properties = PropertiesService.getUserProperties();
  const url = properties.getProperty('API_URL');
  if (!url) throw new Error('Сначала задайте адрес API в меню CashCraft.');
  validateAPIURL_(url);
  const options = {method: method || 'get', muteHttpExceptions: true, followRedirects: false, headers: {Accept: 'application/json'}};
  if (authorized !== false) {
    const token = properties.getProperty('ACCESS_TOKEN');
    const expires = Number(properties.getProperty('EXPIRES_AT'));
    if (!token || !Number.isFinite(expires) || expires <= Date.now() / 1000 + 5) {
      clearSession_();
      throw new Error('Войдите в CashCraft через меню «Регистрация / вход».');
    }
    options.headers.Authorization = 'Bearer ' + token;
  }
  if (payload !== undefined) {
    options.contentType = 'application/json';
    options.payload = JSON.stringify(payload);
  }
  let response;
  try {
    response = UrlFetchApp.fetch(url + path, options);
  } catch (err) {
    throw new Error('API недоступен. Проверьте соединение и адрес API.');
  }
  const status = response.getResponseCode();
  let body;
  try { body = JSON.parse(response.getContentText()); } catch (err) { throw new Error('API вернул некорректный JSON (HTTP ' + status + ').'); }
  if (status === 401) {
    clearSession_();
    throw new Error('Ошибка авторизации. Проверьте данные и выполните вход заново.');
  }
  if (status < 200 || status >= 300) throw new Error('HTTP ' + status + ': ' + (body.error || 'ошибка API'));
  return body;
}

function sendSelectedTransaction() {
  const sheet = selectedSheet_(CASHCRAFT.transactions);
  withDocumentLock_(function() { sendTransactionRow_(sheet, sheet.getActiveRange().getRow()); });
}

function saveSelectedBudget() {
  const sheet = selectedSheet_(CASHCRAFT.budgets);
  withDocumentLock_(function() { saveBudgetRow_(sheet, sheet.getActiveRange().getRow()); });
}

function selectedSheet_(name) {
  const sheet = SpreadsheetApp.getActiveSpreadsheet().getActiveSheet();
  if (sheet.getName() !== name || !sheet.getActiveRange() || sheet.getActiveRange().getRow() < 2) {
    throw new Error('Выберите строку с данными на листе «' + name + '».');
  }
  return sheet;
}

function withDocumentLock_(action) {
  const lock = LockService.getDocumentLock() || LockService.getScriptLock();
  if (!lock.tryLock(20000)) throw new Error('Выполняется другая отправка. Повторите позже.');
  try { return action(); } finally { lock.releaseLock(); }
}

function sendTransactionRow_(sheet, row) {
  const values = sheet.getRange(row, 1, 1, 7).getValues()[0];
  if (values[5] !== '') {
    sheet.getRange(row, 5).setValue(false);
    sheet.getRange(row, 7).setValue('Уже отправлено: ID ' + values[5]);
    return;
  }
  try {
    const tx = transactionFromRow_(values, sheet.getParent().getSpreadsheetTimeZone());
    const saved = requestAPI_('/api/transactions', 'post', tx);
    if (!saved.id) throw new Error('API не вернул ID транзакции. Проверьте список транзакций перед повторной отправкой.');
    sheet.getRange(row, 6).setValue(saved.id);
    sheet.getRange(row, 7).setValue('Сохранено');
  } catch (err) {
    sheet.getRange(row, 7).setValue(safeCellText_(err.message));
    throw err;
  } finally { sheet.getRange(row, 5).setValue(false); }
}

function saveBudgetRow_(sheet, row) {
  try {
    const values = sheet.getRange(row, 1, 1, 2).getValues()[0];
    requestAPI_('/api/budgets', 'post', {category: category_(values[0]), limit: positiveNumber_(values[1], 'Лимит')});
    sheet.getRange(row, 4).setValue('Сохранено');
  } catch (err) {
    sheet.getRange(row, 4).setValue(safeCellText_(err.message));
    throw err;
  } finally { sheet.getRange(row, 3).setValue(false); }
}

function transactionFromRow_(values, timezone) {
  return {date: dateString_(values[0], timezone), amount: positiveNumber_(values[1], 'Сумма'), category: category_(values[2]), description: String(values[3] || '')};
}

function positiveNumber_(value, label) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) throw new Error(label + ' должна быть положительным числом.');
  return value;
}

function category_(value) {
  const category = String(value || '').trim();
  if (!category) throw new Error('Укажите категорию.');
  return category;
}

function dateString_(value, timezone) {
  const date = value instanceof Date ? Utilities.formatDate(value, timezone, 'yyyy-MM-dd') : String(value).trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) throw new Error('Дата должна иметь формат YYYY-MM-DD.');
  const parsed = new Date(date + 'T00:00:00Z');
  if (!Number.isFinite(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== date) throw new Error('Указана несуществующая дата.');
  return date;
}

function safeCellText_(value) {
  const text = String(value);
  return /^[=+\-@]/.test(text) ? "'" + text : text;
}

function installEditTrigger() {
  const ss = SpreadsheetApp.getActiveSpreadsheet();
  const exists = ScriptApp.getProjectTriggers().some(function(trigger) {
    return trigger.getHandlerFunction() === 'cashcraftOnEdit' && trigger.getTriggerSourceId() === ss.getId();
  });
  if (!exists) ScriptApp.newTrigger('cashcraftOnEdit').forSpreadsheet(ss).onEdit().create();
  SpreadsheetApp.getUi().alert('Отправка по флажку включена. Сначала заполните всю строку, затем установите флажок.');
}

function cashcraftOnEdit(e) {
  if (!e || !e.range || e.range.getNumRows() !== 1 || e.range.getNumColumns() !== 1 || e.range.getRow() < 2 || e.value !== 'TRUE') return;
  const sheet = e.range.getSheet();
  const tx = sheet.getName() === CASHCRAFT.transactions && e.range.getColumn() === 5;
  const budget = sheet.getName() === CASHCRAFT.budgets && e.range.getColumn() === 3;
  if (!tx && !budget) return;
  const editor = e.user && e.user.getEmail();
  const owner = Session.getEffectiveUser().getEmail();
  if (!editor || !owner || editor !== owner) {
    sheet.getRange(e.range.getRow(), tx ? 7 : 4).setValue('Для этого редактора используйте отправку выбранной строки через меню CashCraft.');
    e.range.setValue(false);
    return;
  }
  try {
    withDocumentLock_(function() {
      if (e.range.getValue() !== true) return;
      if (tx) sendTransactionRow_(sheet, e.range.getRow());
      else saveBudgetRow_(sheet, e.range.getRow());
    });
  } catch (err) {
    sheet.getRange(e.range.getRow(), tx ? 7 : 4).setValue(safeCellText_(err.message));
  }
}

function getReport() {
  withDocumentLock_(function() {
    const ss = SpreadsheetApp.getActiveSpreadsheet();
    const sheet = ss.getSheetByName(CASHCRAFT.report);
    if (!sheet) throw new Error('Сначала выполните «Подготовить листы».');
    const dates = sheet.getRange('B2:B3').getValues();
    const from = dateString_(dates[0][0], ss.getSpreadsheetTimeZone());
    const to = dateString_(dates[1][0], ss.getSpreadsheetTimeZone());
    if (from > to) throw new Error('Начало периода не может быть позже конца.');
    const report = requestAPI_('/api/reports/summary?from=' + encodeURIComponent(from) + '&to=' + encodeURIComponent(to), 'get');
    if (!Array.isArray(report.categories) || typeof report.total !== 'number') throw new Error('API вернул некорректный отчёт.');
    const rows = report.categories.map(function(item) {
      return [safeCellText_(item.category), item.total, item.has_budget ? item.budget_limit : '', item.has_budget ? item.budget_used_percent / 100 : ''];
    });
    const requiredRows = 7 + rows.length;
    if (requiredRows > sheet.getMaxRows()) sheet.insertRowsAfter(sheet.getMaxRows(), requiredRows - sheet.getMaxRows());
    if (sheet.getLastRow() >= 8) sheet.getRange(8, 1, sheet.getLastRow() - 7, 4).clearContent();
    sheet.getRange('B5').setValue(report.total).setNumberFormat('0.00');
    if (rows.length) {
      sheet.getRange(8, 1, rows.length, 4).setValues(rows);
      sheet.getRange(8, 2, rows.length, 2).setNumberFormat('0.00');
      sheet.getRange(8, 4, rows.length, 1).setNumberFormat('0.00%');
    }
  });
}
