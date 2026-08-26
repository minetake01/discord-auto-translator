# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

A Discord bot that enables people who speak different languages to communicate seamlessly in real time within the same Discord server, using their own native languages.

By linking one channel per language into a **translation group**, any message posted in one channel is automatically translated by an OpenAI-compatible LLM (Chat Completions API) and mirrored across all other channels in the group using the **original sender's name and avatar**. Participants simply read and write in their own language channel for a completely natural conversation.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. User & Server Admin Guide

### Features & User Experience

- **Chat Naturally as Usual**
  No special commands or bot prefixes required. Simply type and send messages normally, and they will be translated and delivered across linked channels in real time.
- **Messages Look Like They Came from the Sender**
  Mirrored messages are delivered via Discord Webhooks with the original author's display name and avatar seamlessly preserved.
- **Full Real-Time Synchronization**
  - **New Messages & Attachments**: Syncs text content as well as images (including image descriptions / alt text) and various file attachments.
  - **Edits & Deletions**: Editing or deleting a message automatically updates or deletes the mirrored messages across all counterpart channels.
  - **Replies**: Quotes a subtext snippet of the referenced message in the target language and links back to the counterpart message (pseudo-reply).
  - **Forwarded Messages**: Syncs forwarded messages with localized headers while preserving the snapshot context.
  - **Reactions & Pins**: Adding/removing emoji reactions and message pins are fully synchronized bidirectionally.
  - **Threads & Forums**: Supports regular threads as well as forum and media channels, including automated tag mapping.
  - **Polls**: Translates poll questions and answers into an Embed format and posts the final results when the poll concludes.
- **"View Original" Message Action**
  Right-click (or long-press on mobile) any translated message, navigate to **Apps → View Original**, and view an ephemeral jump link to the source message along with an excerpt of the original text.
- **Smart Link & Media Rewriting**
  - Links and mentions pointing to managed channels, messages, or threads are automatically rewritten to match the target language counterpart.
  - External website URLs with `hreflang` alternates are automatically swapped for the target-language version.

---

### Getting Started for Server Admins

#### 1. Invite the Bot
Invite the bot to your server using the Discord Developer Portal URL Generator with the following permissions:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **Bot Permissions**:
  - **General**: `View Channels`, `Read Message History`
  - **Text**: `Send Messages`, `Send Messages in Threads`
  - **Moderation**: `Pin Messages`
  - **Webhooks**: `Manage Webhooks`
  - **Threads**: `Create Public Threads`, `Manage Threads`
  - **Reactions**: `Add Reactions`
- **Permissions Integer**: `2252126768139328`
  - *Note: To also sync custom emoji reactions from other servers, grant `Use External Emojis` (Permissions Integer: `2252126768401472`).*

#### 2. Privileged Intent Requirement
Ensure that the `MESSAGE CONTENT INTENT` is enabled in the **Bot** tab of the Discord Developer Portal.

---

### Channel Setup & Basic Operations

#### Create a Translation Group
Run `/new-channel` in your Japanese channel (e.g. `#general-ja`):

```
/new-channel language:ja
```
*Note: If `group` is omitted, the current channel name will be used as the group identifier.*

#### Add Channels in Other Languages
Run `/join-channel` in your English channel (e.g. `#general-en`):

```
/join-channel group:general language:en
```

To add a Chinese channel (e.g. `#general-zh`):

```
/join-channel group:general language:zh-CN
```

Now `#general-ja`, `#general-en`, and `#general-zh` are linked and mutual auto-translation is active.

#### Leaving a Group and Deleting Groups
- Remove a channel from a group: `/leave-channel group:general`
- Delete an entire translation group: `/delete-group group:general`
- View all active translation groups and channels: `/list-groups`

---

### Command Reference

#### Admin Slash Commands
By default, administrative slash commands can only be executed by users with **Administrator permissions**. To grant access to specific roles or members, configure command permissions in Discord: **Server Settings → Integrations → (Bot Name) → Manage → Command Permissions**.

| Command | Description | Key Options |
|---|---|---|
| `/new-channel` | Create a new translation group and register a channel | `language` (required): BCP-47 language code<br>`channel` (optional): Target channel (defaults to current channel)<br>`group` (optional): Group identifier (defaults to channel name) |
| `/join-channel` | Add a channel to an existing translation group | `group` (required): Group identifier<br>`language` (required): BCP-47 language code<br>`channel` (optional): Target channel (defaults to current channel) |
| `/leave-channel` | Remove a channel from a translation group | `group` (required): Group identifier<br>`channel` (optional): Target channel (defaults to current channel) |
| `/delete-group` | Delete an entire translation group | `group` (required): Group identifier to delete |
| `/list-groups` | List all translation groups and linked channels | None |
| `/set-style` | Set translation style or tone for a group | `group` (required): Group identifier<br>`preset` (optional): Style preset (see below)<br>`custom` (optional): Custom natural language instruction (up to 200 chars) |
| `/add-glossary` | Register a preferred term translation in the server glossary | `term` (required): Source term<br>`translation` (required): Target translation<br>`attribute` (optional): Term category (e.g., person name, slang)<br>`always_include` (optional): Include in prompt even without keyword match (default: `false`) |
| `/list-glossary` | List registered glossary entries for this server | None |
| `/remove-glossary`| Remove an entry from the server glossary | `term` (required): Term to remove |
| `/edit-forum-tags` | Edit forum/media tag mappings for a channel in a group | `group` (required): Group identifier<br>`channel` (optional): Target forum channel |
| `/bot-whitelist` | Manage translation allowlist for automated bots and webhooks | Subcommands: `add`, `remove`, `list`<br>`source_type`: `bot` or `webhook`<br>`source_id`: Bot user ID or Webhook ID |

#### Message App Command (Available to All Users)
- **`View Original` (Context Menu)**
  Right-click or long-press any message → **Apps → View Original** to receive an ephemeral jump link and excerpt of the original message.

---

### Advanced Customization

#### 1. Translation Style (`/set-style`)
Tailor the tone and style of translations to match your server's community (`preset` and `custom` are mutually exclusive).

| Preset | Description & Usage |
|---|---|
| `default` | Natural conversational tone as written by native chat users |
| `casual` | Friendly and casual tone suitable for friends and communities |
| `gaming` | Casual gamer slang and gaming community style |
| `friendly` | Warm, polite, and approachable tone |
| `business` | Concise, polite, and professional tone |
| `formal` | Polite and formal tone with honorifics |
| `netslang` | Internet slang and forum-style language |
| `tweet` | Short, punchy social media / microblogging style |
| `literal` | Direct, literal translation when multiple interpretations exist |

#### 2. Server Glossary (`/add-glossary`)
Enforce translations for character names, game terminology, product names, or server-specific jargon (up to 50 entries per server).
- **Attributes (`attribute`)**: Adding categories like "person name", "place name", "slang", "abbreviation", or "technical term" helps the model infer the proper context.
- **Always Include (`always_include`)**: When set to `true`, the term is permanently included in the system prompt context even if the word is not explicitly found in the message body.

#### 3. Forum & Media Tag Mapping (`/edit-forum-tags`)
When linking forum or media channels, you can map forum tags across languages. When a post is published with a tag in one language, the counterpart forum post automatically receives the mapped tag.

#### 4. Automated Message Whitelist (`/bot-whitelist`)
By default, automated messages from bots and webhooks are ignored to prevent infinite loops. You can use `/bot-whitelist add` to allow specific announcement bots, RSS feeds, or integrations to be translated and mirrored.

---

## 2. Engineering & Self-Hosting Guide

### Requirements & Tech Stack

- **Language**: Go 1.24 or later
- **Database**: SQLite (Pure Go driver via `modernc.org/sqlite`, no CGO required)
- **Discord Library**: `github.com/bwmarrin/discordgo`
- **Translation Engine**: OpenAI-compatible Chat Completions API (OpenAI, OpenRouter, Azure OpenAI, local LLMs, etc.)
- **Cross-Compilation**: Fully supported with `CGO_ENABLED=0` for Linux, Windows, and macOS single binaries.

---

### Self-Hosting & Setup

#### 1. Create a Discord Bot
1. Go to the [Discord Developer Portal](https://discord.com/developers/applications) and create a new Application.
2. Under the **Bot** tab, enable `MESSAGE CONTENT INTENT` and copy the Bot Token.
3. Under **OAuth2 → URL Generator**, select `bot` and `applications.commands` scopes with the required permissions, and invite the bot to your server.

#### 2. Prepare an OpenAI-Compatible API
Obtain an API endpoint URL, API key, and model ID from your preferred LLM provider (e.g. OpenAI, OpenRouter, etc.).

#### 3. Configure Environment Variables
Copy `.env.example` to `.env` and fill in the required parameters:

```sh
cp .env.example .env
```

```env
DISCORD_TOKEN=your-discord-bot-token
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=your-openai-api-key
OPENAI_MODEL=your-model-id
# OPENAI_REASONING_EFFORT=none
DB_PATH=./translator.db
HTTP_ADDR=:8080
# PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

#### 4. Build & Run

Run directly:
```sh
go run ./cmd/discord-auto-translator
```

Build and run a standalone binary:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Model Prewarm Validation (`--model-prewarm`)**:
You can validate API credentials, model connectivity, and response schema contracts prior to deployment:
```sh
./discord-auto-translator --model-prewarm
```

---

### Configuration Reference

| Variable | Required | Default | Description |
|---|---|---|---|
| `DISCORD_TOKEN` | **Yes** | - | Discord bot authentication token |
| `OPENAI_BASE_URL` | **Yes** | - | Base URL for OpenAI-compatible Chat Completions (e.g. `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Yes** | - | Bearer API token |
| `OPENAI_MODEL` | **Yes** | - | Target LLM model ID |
| `OPENAI_REASONING_EFFORT` | No | (unset) | Optional `reasoning_effort` for Chat Completions. Set to `none` to disable thinking tokens on hybrid reasoning models |
| `DB_PATH` | No | `./translator.db` | File path for SQLite database |
| `HTTP_ADDR` | No | `:8080` | Address for avatar badge HTTP server |
| `PUBLIC_BASE_URL` | No | (unset) | Public base URL for avatar ring badges. If set, renders top-role color ring around avatars |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | No | `100000` | Token quota per guild per minute for translation API calls |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | No | `120` | Per-IP request limit per minute for the `/avatar` badge endpoint |
| `MESSAGE_LINK_RETENTION_DAYS` | No | `0` | Retention period in days for message link tracking. `0` disables purge; otherwise purges every 24h |
| `GUILD_DATA_RETENTION_DAYS` | No | `0` | Data retention period in days after bot leaves a guild. Rejoining cancels scheduled purge |

---

### Architecture & Design

#### 1. Translation Pipeline
1. **Context Assembly**: Collects channel topic, recent conversation bursts, reply references, shared URL OGP metadata, and scaled image attachments.
2. **Placeholder Masking**: Replaces mentions (`<@id>`), custom emojis (`<:name:id>`), channel tags (`<#id>`), URLs, and code blocks with tokens like `[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]` to prevent prompt injection and broken syntax.
3. **Prompt Composition & Caching**: Assembles system prompt, stable context, history context, and variable message parts structured for provider prefix prompt caching.
4. **Structured Outputs Generation**: Uses `response_format.type=json_schema` (`strict: true`) to generate translations for all target languages in a single LLM round trip.
5. **Post-Processing & Mirroring**: Restores placeholders, rewrites Discord channel/message links to target counterparts, swaps `hreflang` URLs, and fans out webhook posts concurrently.

#### 2. Security & Fail-Closed Behavior
- **Prompt Injection Defense**: All user content is XML-escaped and quarantined within dedicated context sections. Masked placeholders ensure LLM instructions cannot be hijacked.
- **Fail-Closed Principle**: If token limits are exceeded (`finish_reason=length`), invalid JSON is returned, or temporary network failures persist, the bot aborts mirroring and posts a localized notification in the source channel instead of posting corrupted content.

#### 3. Reliability & Data Consistency
- **Idempotency**: Message IDs are tracked via `message_links` and `processed_events` to ensure at-most-once processing under duplicate gateway events.
- **Compensating Transactions**: If database persistence fails after a webhook post, the bot deletes the posted message to prevent orphaned messages.
- **Bidirectional Sync**: Reactions and message pins are mapped across message links so changes in any language channel mirror across the entire group.

---

### Development & Testing

#### Running Tests
```sh
go test ./...
```

#### Multi-Language UI Strings (i18n)
All user-facing strings, errors, and system notifications are defined in `internal/translatorbot/ui_strings.go` across 13 languages. Adding new strings requires entries for all supported languages, validated by `TestUIStringCatalogIsComplete`.

---

## 3. License

This project is licensed under the [MIT License](LICENSE).
