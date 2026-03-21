# MAX Bot Playground

Минимальный каркас чат-бота MAX на Go с двумя режимами запуска:

- `polling` для локальной отладки;
- `webhook` для приближения к production.

## Быстрый старт

PowerShell:

```powershell
$env:GO111MODULE = "on"
$env:GOCACHE = "$PWD\\.gocache"
$env:GOMODCACHE = "$PWD\\.gomodcache"
$env:MAX_BOT_TOKEN = "your_token"
go run .
```

По умолчанию используется `MAX_RUN_MODE=polling`.

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

## Проверка

```powershell
go test ./...
```

## Текущее ограничение

Сценарии и сессии пока in-memory. Интеграция с backend/БД в этот этап не включена.
