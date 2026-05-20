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
```

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
