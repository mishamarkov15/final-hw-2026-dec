# CashCraft

Система учёта личных трат: бюджеты по категориям, контроль лимитов, отчёты за период, импорт и экспорт CSV. Интерфейс — Google Таблицы. Автор: **Михаил Марков**.

Три сервиса на Go: HTTP Gateway, Ledger и Auth; внутреннее взаимодействие — gRPC. Данные пользователей хранятся отдельно в PostgreSQL, миграции запускаются через Goose. Redis кеширует бюджеты и отчёты на 30 секунд; записи сбрасывают соответствующий кеш. Авторизация — JWT.

## Запуск

Нужны Docker и Docker Compose.

```sh
git clone git@github.com:mishamarkov15/final-hw-2026-dec.git
cd final-hw-2026-dec
sh scripts/init-env.sh
docker compose up --build -d
curl http://localhost:8080/ping
```

Ответ проверки — `pong`. Скрипт создаёт локальный `.env` со случайными паролями и ключом JWT; существующий файл не перезаписывается. Параметры — в [.env.example](.env.example). PostgreSQL сохраняет данные в Docker volume. Остановка: `docker compose down`.

## API

[OpenAPI](docs/openapi.yaml) описывает все запросы и ответы; запущенный Gateway также отдаёт спецификацию по `/openapi.yaml`.

```sh
curl -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"demo@example.com","password":"demo-password-2026"}'
```

Скопируйте `access_token` из ответа в переменную `TOKEN`. Повторный вход — тот же запрос на `/api/auth/login`.

```sh
TOKEN='полученный access_token'
curl -X POST http://localhost:8080/api/budgets \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"category":"еда","limit":5000}'
curl -X POST http://localhost:8080/api/transactions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"amount":450,"category":"еда","description":"Обед","date":"2026-11-05"}'
curl 'http://localhost:8080/api/reports/summary?from=2026-11-01&to=2026-11-30' \
  -H "Authorization: Bearer $TOKEN"
```

В отчёте будет `450` и `9%` бюджета. Лимит проверяется по всем расходам категории; период ограничивает только отчёт. Превышение возвращает HTTP `409` и не сохраняет транзакцию. Суммы положительные, до двух знаков после запятой; даты — `YYYY-MM-DD`.

CSV имеет заголовок `amount,category,description,date`; [пример](examples/transactions.csv). Импорт: `POST /api/transactions/import` с `Content-Type: text/csv`; экспорт: `GET /api/transactions/export`. JSON-пакеты принимает `POST /api/transactions/bulk`. До 1000 строк, четыре параллельных обработчика; ошибки отдельных строк возвращаются вместе с числом сохранённых записей.

## Google Таблицы

Подготовлен [шаблон с примерами](https://docs.google.com/spreadsheets/d/1phOEnPeuEYU7haWg5uDyHpyq7hEQeJ2xCffdrlN5qzQ/edit). При работе из другого аккаунта создайте собственную таблицу по инструкции ниже.

Скрипт, настройка меню, отправка по флажку и пример работы — в [google-sheets](google-sheets/README.md). Google нужен публичный HTTPS-адрес API. Для временной демонстрации при запущенном Compose:

```sh
ssh -R 80:localhost:8080 nokey@localhost.run
```

Укажите выданный HTTPS-адрес в меню таблицы **CashCraft → Адрес API**. Туннель действует, пока открыто SSH-соединение; адрес может измениться при переподключении ([документация](https://localhost.run/docs/)). Для постоянной работы разверните Compose на сервере с HTTPS. Альтернативный туннель: `docker compose --profile public up -d tunnel`, адрес — в `docker compose logs tunnel`.

## Проверка

Для тестов нужны Go 1.23+, Node.js 18+ и Python 3.

```sh
go test -race ./... -coverprofile=cover.out
go vet ./...
node --test google-sheets/tests/code.test.cjs
python3 scripts/smoke.py
```

Последняя команда проверяет запущенный API и создаёт отдельные тестовые аккаунты. Для другого адреса: `python3 scripts/smoke.py --base-url https://ваш-адрес`. PostgreSQL-тесты включаются через `TEST_DATABASE_URL` и используют временные схемы. [cover.out](cover.out) — результат тестового покрытия.
