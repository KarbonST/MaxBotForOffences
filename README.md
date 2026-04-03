# MAX Bot Playground

Go-проект для MAX-бота и backend API под ТЗ по обращениям о правонарушениях.

Сейчас в проекте есть 3 основных контура:

- `bot` - MAX-бот с FSM-сценарием подачи обращения;
- `core_api` - backend API для справочников и обращений;
- `postgres` - целевая БД на основе присланной схемы `users/messages/files/...`.

Дополнительно оставлен технический raw-контур:

- `dialog_reports` - JSON-слепки завершённых диалогов;
- файловый outbox - чтобы не терять сырой диалог до нормализации в основную БД.

## Что уже умеет проект

- Бот ведёт пользователя по шагам: категория -> муниципалитет -> телефон -> адрес -> время -> описание -> вложения -> доп. информация -> подтверждение.
- При первом запуске (`bot_started`) и по `/start` бот показывает приветственный текст "О боте" и сразу отдаёт кнопки главного меню.
- Кнопка "О боте" показывает этот же текст и сразу возвращает пользователя в состояние главного меню.
- На шаге телефона бот принимает номер только в формате `11` цифр, начиная с `8` или `7`.
- Кнопка "Поделиться контактом" поддерживается: бот вытаскивает номер из contact payload MAX, включая `vcf_info` / vCard.
- На шаге времени бот валидирует формат `дд/мм/гг чч:мм` (пример: `31/03/26 14:45`).
- Категории и муниципалитеты берутся из PostgreSQL через REST API, а не из хардкода.
- Раздел "Мои сообщения" в боте подключён к backend: бот показывает краткий список обращений пользователя (дата + статус) и открывает полную карточку по номеру из списка.
- После `report:send` бот:
  1. сохраняет raw-слепок диалога в `dialog_reports`;
  2. создаёт реальное обращение в таблице `messages`;
  3. создаёт/обновляет пользователя в `users`;
  4. пишет стартовое событие в `messages_history`;
  5. дожимает raw-слепок, чтобы в `dialog_reports` появились `message_id` и `normalized_at`.
- Backend уже отдаёт API, на которое можно опирать frontend и бота:
  - `GET /api/bot/reference/categories`
  - `GET /api/bot/reference/municipalities`
  - `POST /api/bot/reports`
  - `GET /api/bot/reports`
  - `GET /api/bot/reports/by-user/{maxUserId}`
  - `GET /api/bot/reports/{id}`

## Архитектура

```text
MAX -> bot -> core_api -> PostgreSQL
             \-> dialog_reports (raw snapshot / outbox)

frontend -> core_api -> PostgreSQL
```

Правило источника истины:

- `users`, `messages`, `messages_history`, `files`, `clarification_requests` - основная бизнес-модель;
- `dialog_reports` - только технический raw/audit слой.

## Локальный запуск через Docker Compose

Локальный стек описан в [docker-compose.yml](docker-compose.yml):

- `postgres`
- `core-api`
- `bot`

Подготовка:

1. Скопируйте [compose.env.example](compose.env.example) в `.env`.
2. Заполните `MAX_BOT_TOKEN`.
3. Поднимите стек:

```bash
docker compose up --build
```

Полезные команды:

```bash
docker compose logs -f core-api
docker compose logs -f bot
docker compose down
docker compose down -v
```

При первом старте PostgreSQL инициализируется из:

- [000_core_schema.sql](deploy/postgres/init/000_core_schema.sql) - основная схема под ТЗ;
- [001_reference_schema.sql](deploy/postgres/init/001_reference_schema.sql) - seed категорий и муниципалитетов;
- [002_dialog_reports.sql](deploy/postgres/init/002_dialog_reports.sql) - raw-таблица слепков диалога.

## Быстрый запуск без Docker

Нужна PostgreSQL с применённой схемой и сидом.

### 1. Запуск backend API

```bash
export DATABASE_URL="postgres://maxbot:maxbot@127.0.0.1:5432/maxbot?sslmode=disable"
export CORE_API_ADDR=":8091"
go run ./cmd/core_api
```

### 2. Запуск бота

```bash
export MAX_BOT_TOKEN="your_token"
export MAX_RUN_MODE="polling"
export REFERENCE_API_BASE="http://127.0.0.1:8091"
export CORE_API_BASE="http://127.0.0.1:8091"
export REPORT_PIPELINE_ENABLED="true"
export REPORT_DATABASE_URL="postgres://maxbot:maxbot@127.0.0.1:5432/maxbot?sslmode=disable"
go run .
```

## Core API

`core_api` - основной backend для бота и будущего frontend.

### Endpoint-ы

- `GET /healthz`
- `GET /api/bot/reference/categories`
- `GET /api/bot/reference/municipalities`
- `POST /api/bot/reports`
- `GET /api/bot/reports`
- `GET /api/bot/reports/by-user/{maxUserId}`
- `GET /api/bot/reports/{id}`

### Создание обращения

Пример запроса:

```bash
curl -X POST http://127.0.0.1:8091/api/bot/reports \
  -H 'Content-Type: application/json' \
  -d '{
    "dialog_dedup_key": "dlg-777:raw-1",
    "max_user_id": 777,
    "category_id": 1,
    "municipality_id": 2,
    "phone": "89991234567",
    "address": "ул. Мира, 1",
    "incident_time": "31/03/26 14:45",
    "description": "Описание нарушения",
    "additional_info": "Доп. сведения"
  }'
```

Пример ответа:

```json
{
  "id": 15,
  "report_number": "15",
  "status": "moderation",
  "stage": "sended",
  "user_id": 11,
  "max_user_id": 777,
  "created_at": "2026-03-29T10:00:00Z",
  "sended_at": "2026-03-29T10:00:00Z",
  "updated_at": "2026-03-29T10:00:00Z"
}
```

### Что делает backend при `POST /api/bot/reports`

- валидирует и нормализует входные данные;
- нормализует телефон (если пришёл в формате `8XXXXXXXXXX`/`7XXXXXXXXXX`, приводит к хранимому `10`-значному виду);
- ищет/создаёт пользователя по `users.max_id`;
- создаёт запись в `messages` со статусом `moderation` и этапом `sended`;
- пишет событие в `messages_history`;
- если передан `dialog_dedup_key`, пытается сразу связать raw-запись из `dialog_reports` с созданным `messages.id`, а raw outbox потом безопасно дожимает эту связь повторным upsert.

## Raw snapshot: dialog_reports

Таблица `dialog_reports` не заменяет основную модель. Она нужна для:

- аудита завершённых диалогов;
- повторной обработки, если нормализация в `messages` временно упала;
- дедупликации финальной отправки.

Схема raw-таблицы сейчас хранит:

- `dedup_key`
- `dialog_id`
- `user_id`
- `report_number`
- `message_id`
- `normalized_at`
- `payload JSONB`

То есть связь теперь такая:

`dialog_reports.payload` -> технический слепок  
`messages` -> реальное обращение для работы системы

## ENV

Бот:

- `TZ` - пользовательская таймзона контейнера, по умолчанию `Europe/Moscow`
- `MAX_BOT_TOKEN` - обязателен
- `MAX_RUN_MODE` - `polling` или `webhook`
- `MAX_API_BASE` - base URL MAX API
- `REFERENCE_API_BASE` - где бот берёт справочники, по умолчанию `http://127.0.0.1:8091`
- `REFERENCE_API_TIMEOUT`
- `REFERENCE_CACHE_TTL`
- `CORE_API_BASE` - backend API обращений, по умолчанию `http://127.0.0.1:8091`
- `CORE_API_TIMEOUT`
- `REPORT_PIPELINE_ENABLED`
- `REPORT_DATABASE_URL`
- `REPORT_OUTBOX_DIR`
- `REPORT_OUTBOX_QUEUE_SIZE`
- `REPORT_OUTBOX_RETRY_BASE`
- `REPORT_OUTBOX_RETRY_MAX`

Backend:

- `TZ` - таймзона контейнера `core_api`, по умолчанию `Europe/Moscow`
- `DATABASE_URL` - обязателен для `core_api`
- `CORE_API_ADDR`
- `CORE_API_READ_TIMEOUT`
- `CORE_API_WRITE_TIMEOUT`
- `CORE_API_SHUTDOWN_TIMEOUT`

## Структура проекта

- [max_bot.go](max_bot.go) - точка входа MAX-бота
- [cmd/core_api](cmd/core_api) - основной backend API
- [cmd/reference_api](cmd/reference_api) - legacy/standalone API только для справочников
- [internal/reporting](internal/reporting) - доменная модель обращений, Postgres store, HTTP handler, HTTP client, тесты
- [internal/reference](internal/reference) - справочники из PostgreSQL
- [internal/scenario](internal/scenario) - FSM сценария бота
- [internal/report](internal/report) - raw JSON payload, outbox и PostgreSQL sink
- [deploy/postgres/init](deploy/postgres/init) - init SQL для локального окружения
- [docker-compose.yml](docker-compose.yml) - локальный стек `postgres + core-api + bot`

## Тесты

```bash
go test ./...
```

Покрыты:

- нормализация и валидация `CreateReportRequest`;
- запись обращения в `users/messages/messages_history`;
- HTTP handler `core_api`;
- FSM финального шага отправки;
- конфиг и runtime-слой.

## Текущие ограничения

- Пользовательские сессии бота всё ещё in-memory.
- Вложения пока не нормализуются в таблицу `files`.
- Статусы, уточнения и уведомления реализованы пока не полностью.
