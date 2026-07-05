# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

一個讓使用不同語言的使用者能在同一個 Discord 伺服器中一起聊天的機器人。

為每種語言準備一個頻道，並將它們連結為一個**翻譯群組**。當某個頻道有新訊息時，Google Gemini 會立即翻譯並鏡像到群組內所有其他頻道 — 保留原發送者的名稱與頭像 — 讓每個頻道讀起來都像是以該語言進行的自然對話。

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

## 功能特色

- **全面同步** — 不只是新訊息：編輯、刪除、回覆、轉發訊息、表情反應、釘選、討論串（文字 / 論壇 / 媒體頻道）以及僅含附件的訊息，都會在整個群組內鏡像同步。
- **訊息如同本人發送** — 鏡像訊息透過 Webhook 發送，顯示原作者的名稱與頭像。
- **自然的翻譯** — Gemini 會參考頻道名稱、主題與最近的對話紀錄作為上下文；每個伺服器還可設定詞彙表，為人名與專業術語指定固定譯法。
- **智慧連結處理** — 指向受管理頻道或訊息的連結與提及會被改寫為各語言頻道的對應目標；具有 `hreflang` 替代版本的 URL 會替換為目標語言版本。
- **高效且安全** — 沒有可翻譯文字的訊息（僅含 URL、提及、自訂表情、程式碼）會直接鏡像而不呼叫 Gemini API；每個伺服器有權杖速率限制；URL、提及與程式碼區塊受到提示詞注入防護。
- **在地化介面** — 指令回應依使用者的 Discord 用戶端語言顯示，頻道通知使用頻道設定的語言（支援 13 種語言，未支援語言以英文顯示）。

## 前置需求

- Go 1.24 或更新版本
- 已啟用 `MESSAGE CONTENT` 特權 Intent 的 Discord 機器人帳號
- Google Gemini API 金鑰

## 安裝設定

### 1. 準備 Discord 機器人

1. 在 [Discord Developer Portal](https://discord.com/developers/applications) 建立應用程式
2. 在 **Bot** 頁面：
   - 啟用 `MESSAGE CONTENT INTENT`（必要）
   - 複製機器人權杖
3. 透過 **OAuth2 → URL Generator** 邀請機器人加入伺服器：
   - Scopes: `bot`, `applications.commands`
   - Permissions（依 Developer Portal 中的名稱）：
     - **一般**: `View Channel`, `Read Message History`
     - **訊息**: `Send Messages`, `Send Messages in Threads`
     - **管理**: `Pin Messages`
     - **Webhook**: `Manage Webhooks`
     - **討論串**: `Create Public Threads`, `Manage Threads`
     - **表情反應**: `Add Reactions`
   - 上述權限的整數值為 `2252126768139328`
   - 若也要同步來自其他伺服器的自訂表情反應，請額外授予 `Use External Emojis`，此時權限整數值為 `2252126768401472`

### 2. 取得 Gemini API 金鑰

請在 [Google AI Studio](https://aistudio.google.com/) 取得 API 金鑰。

### 3. 設定環境變數

```sh
cp .env.example .env
```

編輯 `.env` 並設定以下內容：

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
GEMINI_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
```

| 變數 | 必要 | 說明 |
|---|---|---|
| `DISCORD_TOKEN` | 必要 | Discord 機器人權杖 |
| `GEMINI_API_KEY` | 必要 | Gemini API 金鑰 |
| `DB_PATH` | 選用 | SQLite 檔案路徑（預設: `./translator.db`） |
| `HTTP_ADDR` | 選用 | 頭像徽章伺服器位址（預設: `:8080`） |
| `PUBLIC_BASE_URL` | 選用 | 頭像環徽章的公開基礎 URL。未設定時，鏡像訊息使用 Discord 原始頭像 URL，不會使用徽章伺服器 |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | 選用 | 每個伺服器每分鐘的 Gemini 權杖上限（預設: `100000`） |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 選用 | `/avatar` 徽章端點的每 IP 每分鐘請求上限（預設: `120`） |

### 4. 啟動

```sh
go run ./cmd/discord-auto-translator
```

或先建置再執行：

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## 使用方式

機器人啟動後，斜線指令會在各伺服器中註冊。

### 設定頻道

#### 建立翻譯群組

在日文頻道中執行 `/new-channel` 建立翻譯群組：

```
/new-channel language:ja
```

#### 加入其他語言頻道

在英文頻道中執行 `/join-channel` 將其加入群組：

```
/join-channel group:general language:en
```

若要再加入中文頻道：

```
/join-channel group:general language:zh-TW
```

這樣 `#general-ja`、`#general-en`、`#general-zh` 就連結在一起了。

### 指令一覽

預設情況下，管理用斜線指令僅**伺服器管理員**可以執行。若要允許其他身分組使用，請在 Discord 的「伺服器設定」→「整合」→ 該機器人的「管理」→「指令權限」中，進行全域或依指令的授權設定。機器人不會自行變更身分組或指令權限。

| 指令 | 說明 |
|---|---|
| `/new-channel language:[語言] channel:<頻道> group:<群組>` | 建立新的翻譯群組。省略 `channel` 時使用目前頻道；省略 `group` 時使用頻道名稱 |
| `/join-channel group:[群組] language:[語言] channel:<頻道>` | 將頻道加入群組。省略 `channel` 時使用目前頻道 |
| `/leave-channel group:[群組] channel:<頻道>` | 使頻道退出群組。省略 `channel` 時使用目前頻道 |
| `/delete-group group:[群組]` | 刪除整個群組 |
| `/list-groups` | 列出此伺服器的翻譯群組及其頻道 |
| `/add-glossary term:[詞彙] translation:[譯文] attribute:<屬性> always_include:<布林>` | 在伺服器詞彙表註冊優先譯法。`attribute` 為附候選的自由輸入；`always_include` 預設為 `false` |
| `/list-glossary` | 列出伺服器的詞彙表 |
| `/remove-glossary term:[詞彙]` | 刪除詞彙表項目 |
| `/set-style group:[群組] preset:<預設> custom:<自訂指示>` | 設定群組的翻譯風格。指定 `preset` 或 `custom` 其中之一，不可同時指定 |

- `language` 使用 BCP-47 格式（如 `en`、`ja`、`zh-CN`、`pt-BR`、`ko`、`fr`）
- 每個伺服器最多可註冊 50 筆詞彙
- `attribute` 會提示「人名」「地名」「俚語」「縮寫」「專業術語」等候選，也可自由輸入任意屬性。指定的屬性將作為 Gemini 判斷詞彙含義的上下文
- 一般詞彙僅在待翻譯內文包含 `term`（不分大小寫）時才會加入系統指令；`always_include:true` 的詞彙則永遠加入
- 省略 `channel` 選項時，指令作用於執行指令的頻道
- 支援的頻道類型：文字、公告、論壇、媒體

## 測試

```sh
go test ./...
```

## 部署到 GCE

`deploy/deploy-gce.ps1` 中包含用於 Google Compute Engine 的部署指令碼（Windows PowerShell）。

從範例建立 `deploy/deploy.json` 以設定 GCE 連線。應用程式設定與密鑰預設使用 `.env`；可透過 `deploy.json` 的 `envFile` 或 `-EnvFile` 指定其他檔案。

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# 編輯 deploy.json 與 .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # 首次設定
.\deploy\deploy-gce.ps1                          # 僅更新程式碼
.\deploy\deploy-gce.ps1 -UploadEnv               # 更新密鑰
```

## 授權條款

本專案的授權條款請參閱 [LICENSE](LICENSE) 檔案。
