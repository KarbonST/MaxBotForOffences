# MAX Bot Playground

Минимальный каркас чат-бота MAX на Go с двумя режимами запуска:

- `polling` для локальной отладки;
- `webhook` для приближения к production.

Справочники категорий и муниципалитетов больше не захардкожены в коде бота. Теперь бот читает их через REST API, а API поднимается отдельным процессом и берёт данные из PostgreSQL.

## Быстрый старт

Сейчас проект запускается в 2 процесса:

1. `reference_api` поднимает REST API над таблицами `categories` и `municipalities` в PostgreSQL.
2. `max_bot` запускает самого бота и читает справочники через HTTP.

Минимальный порядок запуска:

1. Поднять PostgreSQL со схемой, где есть таблицы `categories` и `municipalities`.
2. Запустить `reference_api`.
3. Запустить бота с `MAX_BOT_TOKEN`.

PowerShell:

```powershell
$env:GO111MODULE = "on"
$env:GOCACHE = "$PWD\\.gocache"
$env:GOMODCACHE = "$PWD\\.gomodcache"
$env:REFERENCE_API_BASE = "http://127.0.0.1:8090"
$env:MAX_BOT_TOKEN = "your_token"
go run .
```

По умолчанию используется `MAX_RUN_MODE=polling`.

## Reference API

Отдельный процесс, который читает таблицы `categories` и `municipalities` из PostgreSQL и публикует:

- `GET /healthz`
- `GET /api/reference/categories`
- `GET /api/reference/municipalities`

Запуск:

```powershell
$env:DATABASE_URL = "postgres://user:password@localhost:5432/maxbot?sslmode=disable"
$env:REFERENCE_API_ADDR = ":8090"
go run ./cmd/reference_api
```

Примеры запросов:

```bash
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/api/reference/categories
curl http://127.0.0.1:8090/api/reference/municipalities
```

Формат ответа:

```json
{
  "items": [
    {
      "id": 1,
      "sorting": 1,
      "name": "Благоустройство"
    }
  ]
}
```

Ожидаемая схема таблиц:

- `categories(id, sorting, name, is_active)`
- `municipalities(id, sorting, name, is_active)`

API отдаёт только записи с `is_active = true`, отсортированные по `sorting`.

Ключевые ENV:

- `DATABASE_URL` (required)
- `REFERENCE_API_ADDR` (default: `:8090`)
- `REFERENCE_API_READ_TIMEOUT`
- `REFERENCE_API_WRITE_TIMEOUT`
- `REFERENCE_API_SHUTDOWN_TIMEOUT`

## Режим Polling

```powershell
$env:MAX_RUN_MODE = "polling"
$env:MAX_POLL_TIMEOUT = "30"
$env:MAX_POLL_LIMIT = "100"
$env:MAX_POLL_ONCE = "0"
$env:MAX_POLL_MAX_CYCLES = "0"
$env:MAX_LOG_EMPTY_POLLS = "1"
go run .
```

Короткий прогон (один цикл):

```powershell
$env:MAX_POLL_ONCE = "1"
go run .
```

## Режим Webhook

```powershell
$env:MAX_RUN_MODE = "webhook"
$env:MAX_WEBHOOK_ADDR = ":8080"
$env:MAX_WEBHOOK_PATH = "/webhook/max"
$env:MAX_WEBHOOK_SECRET = "optional_secret"
go run .
```

Служебные endpoint-ы в webhook режиме:

- `GET /healthz`
- `GET /readyz`
- `POST /webhook/max` (или ваш `MAX_WEBHOOK_PATH`)

## Ключевые ENV

Базовые:

- `MAX_BOT_TOKEN` (required)
- `MAX_API_BASE` (default: `https://platform-api.max.ru`)
- `MAX_RUN_MODE` (`polling` or `webhook`)
- `REFERENCE_API_BASE` (default: `http://127.0.0.1:8090`)
- `REFERENCE_API_TIMEOUT`
- `REFERENCE_CACHE_TTL`

Polling:

- `MAX_POLL_TIMEOUT` (seconds)
- `MAX_POLL_LIMIT`
- `MAX_POLL_ONCE` (`0/1`)
- `MAX_POLL_MAX_CYCLES` (0 = unlimited)
- `MAX_LOG_EMPTY_POLLS` (`0/1`)

Webhook/HTTP:

- `MAX_WEBHOOK_ADDR`
- `MAX_WEBHOOK_PATH`
- `MAX_WEBHOOK_SECRET`
- `MAX_WEBHOOK_QUEUE_SIZE`
- `MAX_HTTP_READ_TIMEOUT` (duration, e.g. `10s`, or seconds as integer)
- `MAX_HTTP_WRITE_TIMEOUT`
- `MAX_SHUTDOWN_TIMEOUT`

Логи:

- `LOG_FORMAT` (`text` or `json`)
- `LOG_LEVEL` (`debug`, `info`, `warn`, `error`)

Retry/Dedup:

- `MAX_API_MAX_RETRIES`
- `MAX_API_RETRY_BASE_MS`
- `MAX_API_RETRY_MAX_MS`
- `MAX_DEDUP_TTL` (duration)

## Структура проекта

- `max_bot.go` - точка входа бота MAX
- `cmd/reference_api` - отдельный HTTP API для справочников из PostgreSQL
- `internal/reference` - Postgres store, HTTP handler, клиент и кэш справочников
- `internal/scenario` - FSM и шаблоны диалога бота
- `internal/maxapi` - клиент официального MAX Bot API
- `internal/runtime` - webhook/polling источники обновлений и deduplication

## Проверка

```powershell
go test ./...
```

## Текущее ограничение

Сценарии и сессии пока in-memory. В PostgreSQL через REST API сейчас вынесены только справочники категорий и муниципалитетов; создание обращений и работа с пользовательскими сообщениями ещё не подключены к backend.
