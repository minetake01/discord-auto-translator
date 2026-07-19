# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

A Discord bot that lets people who speak different languages chat together in the same server.

Link one channel per language into a **translation group**. Every message posted in one channel is translated by Amazon Bedrock `google.gemma-4-26b-a4b` and mirrored to all the other channels in the group — with the original sender's name and avatar — so each channel reads like a normal conversation in its own language.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

## Features

- **Everything stays in sync** — not just new messages: edits, deletions, replies, forwarded messages, reactions, pins, threads (text / forum / media channels), and attachment-only messages are all mirrored across the group.
- **Messages look like they came from the sender** — mirrored messages are delivered via webhooks with the original author's name and avatar.
- **Natural translations** — Gemma 4 26B-A4B sees the channel name, topic, and recent conversation history as context, and a per-server glossary lets you enforce preferred translations for names and jargon.
- **Smart link handling** — links and mentions pointing to managed channels or messages are rewritten to each language's counterpart, and URLs with `hreflang` alternates are swapped for the target-language version.
- **Efficient and safe** — messages with nothing to translate (URLs, mentions, custom emojis, code) are mirrored without calling the translation API, per-server token rate limits apply, and URLs / mentions / code blocks are shielded against prompt injection. Translation failures are fail-closed (no mirror, localized notification in the source channel).
- **Localized UI** — command responses follow each user's Discord client language, and channel notifications use the channel's configured language (13 languages, English fallback).

## Requirements

- Go 1.24 or later
- A Discord bot account with the `MESSAGE CONTENT` privileged intent enabled
- An AWS account with Amazon Bedrock model access
- An IAM access key allowed to create inference on the Bedrock Mantle default Project in `us-west-2`

## Setup

### 1. Prepare the Discord bot

1. Create an application in the [Discord Developer Portal](https://discord.com/developers/applications)
2. On the **Bot** page:
   - Enable the `MESSAGE CONTENT INTENT` (required)
   - Copy the bot token
3. Invite the bot to your server via **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permissions (as shown in the Developer Portal):
     - **General**: `View Channel`, `Read Message History`
     - **Messages**: `Send Messages`, `Send Messages in Threads`
     - **Moderation**: `Pin Messages`
     - **Webhooks**: `Manage Webhooks`
     - **Threads**: `Create Public Threads`, `Manage Threads`
     - **Reactions**: `Add Reactions`
   - The permissions integer for the above is `2252126768139328`
   - To also sync custom emoji reactions from other servers, additionally allow `Use External Emojis`; the permissions integer then becomes `2252126768401472`

### 2. Configure Amazon Bedrock

1. Switch the AWS console to `us-west-2`, open `google.gemma-4-26b-a4b` from the Amazon Bedrock model catalog as an administrator, and run it once in the playground. Current Bedrock model access is enabled by default and any required third-party Marketplace agreement is initiated by the first invocation, so there may be no separate Enable button. Agreement processing can take up to about 15 minutes.
2. Create a dedicated IAM user for the bot and attach the AWS managed policy `AmazonBedrockMantleInferenceAccess`. After validation, you can replace it with a custom policy scoped to the actual Project ARN.
3. Create an access key for that user and set `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` in `.env`. Never use a root-user access key.

The model, region, timeout, and output limit are fixed in code. Optionally tune per-guild token throughput with `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` (default `100000`).

### 3. Configure environment variables

```sh
cp .env.example .env
```

Edit `.env` and set the following:

```env
DISCORD_TOKEN=your-discord-bot-token
AWS_ACCESS_KEY_ID=your-aws-access-key-id
AWS_SECRET_ACCESS_KEY=your-aws-secret-access-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

| Variable | Required | Description |
|---|---|---|
| `DISCORD_TOKEN` | Yes | Discord bot token |
| `AWS_ACCESS_KEY_ID` | Yes | Access key ID for the dedicated Bedrock IAM user |
| `AWS_SECRET_ACCESS_KEY` | Yes | Secret access key for the dedicated Bedrock IAM user |
| `DB_PATH` | No | Path to the SQLite file (default: `./translator.db`) |
| `HTTP_ADDR` | No | Address of the avatar badge server (default: `:8080`) |
| `PUBLIC_BASE_URL` | No | Public base URL for avatar ring badges. If unset, mirrored messages use the original Discord avatar URL and the badge server is not used |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | No | Per-guild Gemma 4 26B-A4B token limit per minute (default: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | No | Per-IP request limit per minute for the `/avatar` badge endpoint (default: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | No | Days to retain `message_links` in SQLite before automatic purge. `0` (default) disables purging; set e.g. `60` to delete links older than 60 days at startup and every 24 hours |
| `GUILD_DATA_RETENTION_DAYS` | No | Days to retain a guild's SQLite data after the bot is removed from that guild. `0` (default) disables purging; set e.g. `30` to purge data for guilds removed more than 30 days ago at startup and every 24 hours. Rejoining before expiry cancels the scheduled purge |

### Amazon Bedrock operational contract

Translation uses the non-streaming Mantle Responses API with model `google.gemma-4-26b-a4b` in `us-west-2`. Fixed request parameters are a **30 s** runtime timeout, **provider-default temperature 1.0**, **max_output_tokens 4096**, and `store=false`. Gemma 4 does not support Bedrock Structured Outputs, so the fixed JSON Schema is included in the system instruction and the bot strictly validates the resulting multi-language `translations` array.

The bot sends one request for all target languages. If the 4K output limit is reached, Bedrock stops for any non-normal reason, or the response has malformed JSON, missing or reordered language tags, empty translations, or extra fields, the whole translation fails. There is no retry, request splitting, provider fallback, or compatibility path.

The bot does not log prompts, responses, credentials, or provider error messages. Mantle does not support per-request Bedrock metadata, so no Discord IDs are sent as metadata. Safe provider diagnostics are limited to the error type, code, parameter name, and request ID. Translation failures and rate-limit breaches are **fail-closed** — the message is not mirrored and the source channel receives a localized notification.

Before replacing a running binary, the GCE deployment script invokes `--bedrock-prewarm` with a five-minute deadline to validate credentials, model access, and the response contract. The service itself applies the 30-second runtime deadline.

### 4. Run

```sh
go run ./cmd/discord-auto-translator
```

Or build and run:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Usage

Once the bot starts, slash commands are registered in each server.

### Setting up channels

#### Create a translation group

Run `/new-channel` in your Japanese channel to create a translation group:

```
/new-channel language:ja
```

#### Add channels in other languages

Run `/join-channel` in your English channel to add it to the group:

```
/join-channel group:general language:en
```

To add a Chinese channel as well:

```
/join-channel group:general language:zh-CN
```

Now `#general-ja`, `#general-en`, and `#general-zh` are linked.

### Commands

By default, the admin slash commands can only be run by **server administrators**. To allow additional roles, go to Discord's "Server Settings" → "Integrations" → the bot's "Manage" → "Command Permissions" and grant access globally or per command. The bot never changes roles or command permissions on its own.

| Command | Description |
|---|---|
| `/new-channel language:[lang] channel:<channel> group:<group>` | Create a new translation group. `channel` defaults to the current channel; `group` defaults to the channel name |
| `/join-channel group:[group] language:[lang] channel:<channel>` | Add a channel to a group. `channel` defaults to the current channel |
| `/leave-channel group:[group] channel:<channel>` | Remove a channel from a group. `channel` defaults to the current channel |
| `/delete-group group:[group]` | Delete an entire group |
| `/list-groups` | List translation groups and their channels for this server |
| `/add-glossary term:[term] translation:[translation] attribute:<attribute> always_include:<bool>` | Register a preferred translation in the server glossary. `attribute` is free-form with suggestions; `always_include` defaults to `false` |
| `/list-glossary` | List the server's glossary entries |
| `/remove-glossary term:[term]` | Remove a glossary entry |
| `/set-style group:[group] preset:<preset> custom:<custom>` | Set translation style for a group. Specify `preset` or `custom`, not both |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | Allow an automated message source in this server. For `source_type:bot`, `source_id` is the bot user ID; for `source_type:webhook`, it is the webhook ID |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | Remove the matching automated message source from this server's allowlist |
| `/bot-whitelist list` | List the bot and webhook sources allowed in this server |

- Source allowlists are persisted in SQLite and scoped to each Discord server (guild). Translator-managed output webhooks and messages from this translator bot itself remain excluded even if their IDs are added

- `language` uses BCP-47 codes (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, etc.)
- Up to 50 glossary entries can be registered per server
- `attribute` suggests "person name", "place name", "slang", "abbreviation", and "technical term", but any value can be entered. The attribute is used as context for Gemma 4 26B-A4B to understand the term's meaning
- Regular terms are added to the system instructions only when the message body contains `term` (case-insensitive). Terms with `always_include:true` are always added
- If the `channel` option is omitted, the command applies to the channel it was run in
- Supported channel types: text, news, forum, and media

## Testing

```sh
go test ./...
```

## Deploying to GCE

A deployment script for Google Compute Engine is included at `deploy/deploy-gce.ps1` (Windows PowerShell).

Create `deploy/deploy.json` from the example for GCE connection settings. App settings and secrets use `.env` by default; set `envFile` in `deploy.json` or pass `-EnvFile` to use a different file.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# Edit deploy.json and .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # First-time setup
.\deploy\deploy-gce.ps1                          # Code updates only
.\deploy\deploy-gce.ps1 -UploadEnv               # Update secrets
```

## License

See the [LICENSE](LICENSE) file for this project's license.
