# Discord Auto Translator

Discord channels in the same translation group are mirrored through webhooks and translated with Gemini.

## Directory Layout

- `cmd/discord-auto-translator`: application entry point.
- `internal/translatorbot`: bot configuration, Discord integration, translation service, persistence, and tests.

## Setup

1. Copy `.env.example` to `.env`.
2. Fill in the required values:

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
```

`PUBLIC_BASE_URL` must be reachable from Discord. The bot uses it to expose `/avatar`, which returns a PNG avatar with an orange circular border for webhook messages.

## Run

```sh
go run ./cmd/discord-auto-translator
```

## Test

```sh
go test ./...
```

## Build

```sh
go build ./cmd/discord-auto-translator
```
