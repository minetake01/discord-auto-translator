# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

讓使用不同語言的成員能夠在同一個 Discord 伺服器中，以各自的母語進行即時無縫交流的 Discord 機器人。

為每種語言建立一個頻道並將其連結為一個**翻譯群組**後，在任一頻道發送的訊息都會透過相容 OpenAI 的大型語言模型（Chat Completions API）自動翻譯，並以**原發言者的名稱與頭像**鏡像轉發至群組內所有其他語言頻道。成員只需在自己的母語頻道中閱讀與發言，即可享受自然的跨語言交流體驗。

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. 使用者與伺服器管理員指南

### 主要特色與使用者體驗

- **如同平常一般自然聊天**
  無需輸入任何特殊指令或前綴。和平常一樣發送訊息，機器人就會自動將其翻譯並同步發送至其他語言頻道。
- **完整保留發言者身份**
  鏡像訊息透過 Webhook 發送，完整保留原發言者的顯示名稱與頭像。
- **全方位即時雙向同步**
  - **新訊息與附件**: 不僅支援文字，亦完整支援圖片（包含圖片替代文字 / Alt text）及各類檔案附件。
  - **訊息編輯與刪除**: 編輯或刪除原訊息時，其他語言頻道的翻譯訊息會即時同步更新或刪除。
  - **回覆（引用）**: 在目標語言中附帶被引用訊息的摘要並生成對應跳轉連結（偽引用形式）。
  - **轉發訊息**: 保留轉發內容並對轉發標題進行在地化展示。
  - **表情回應與置頂**: 對訊息新增/移除表情回應（Reaction）以及置頂（Pin）操作皆雙向同步。
  - **討論串與論壇**: 支援一般討論串（Thread）以及論壇（Forum）與媒體頻道，包含論壇標籤（Tag）的自動對應。
  - **投票（Polls）**: 投票訊息會自動翻譯問題與選項，以 Embed 形式鏡像展示，並在投票結束時通知最終結果。
- **「View Original」（查看原文）功能**
  右鍵點擊（手機端長按）任一翻譯訊息，選擇 **「應用程式 (Apps)」 → 「View Original」**，即可查看原訊息的跳轉連結與原文預覽（僅自己可見）。
- **智慧連結與媒體轉換**
  - 訊息中指向群組內其他頻道、訊息或討論串的連結與提及，會自動改寫為目標語言頻道的對應 ID。
  - 指向外部網站的 URL 若包含 `hreflang` 多語言版本，會自動替換為目標語言對應的 URL。

---

### 伺服器加入步驟

#### 1. 邀請機器人至伺服器
在 Discord 開發者後台（Developer Portal）生成邀請連結並賦予以下權限：

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **機器人權限（Bot Permissions）**:
  - **一般**: `View Channels`（檢視頻道）, `Read Message History`（讀取訊息歷史）
  - **文字**: `Send Messages`（發送訊息）, `Send Messages in Threads`（在討論串中發送訊息）
  - **管理**: `Pin Messages`（置頂訊息）
  - **Webhook**: `Manage Webhooks`（管理 Webhook）
  - **討論串**: `Create Public Threads`（建立公開討論串）, `Manage Threads`（管理討論串）
  - **表情回應**: `Add Reactions`（新增表情回應）
- **權限數值 (Permissions Integer)**: `2252126768139328`
  - *註：若需要同步來自其他外部伺服器的自訂表情，請勾選 `Use External Emojis`（權限數值: `2252126768401472`）。*

#### 2. 開啟特權 Intent
確保在 Discord 開發者後台的 **Bot** 頁面中啟用了 `MESSAGE CONTENT INTENT`（訊息內容特權 Intent）。

---

### 頻道設定（基本操作）

#### 建立翻譯群組
在日語頻道（例如 `#general-ja`）執行 `/new-channel` 建立翻譯群組：

```
/new-channel language:ja
```
*註：若省略 `group`，預設使用當前頻道名稱作為群組識別碼。*

#### 加入其他語言頻道
在英語頻道（例如 `#general-en`）執行 `/join-channel` 加入該群組：

```
/join-channel group:general language:en
```

加入中文頻道（例如 `#general-zh`）：

```
/join-channel group:general language:zh-TW
```

完成後，`#general-ja`、`#general-en` 與 `#general-zh` 將互相連結並開啟自動翻譯。

#### 退出頻道與刪除群組
- 將頻道移出群組: `/leave-channel group:general`
- 完全刪除翻譯群組: `/delete-group group:general`
- 檢視當前伺服器中的翻譯群組與頻道配置: `/list-groups`

---

### 指令參考

#### 管理員斜線指令
管理指令預設僅限**伺服器管理員（Administrator 權限）**執行。如需授權其他身分組，可在 Discord 的「伺服器設定」→「整合」→「機器人管理」→「指令權限」中設定。

| 指令 | 說明 | 主要選項 |
|---|---|---|
| `/new-channel` | 建立新的翻譯群組並註冊頻道 | `language`（必填）: BCP-47 語言代碼<br>`channel`（選填）: 目標頻道（預設當前頻道）<br>`group`（選填）: 群組識別碼（預設頻道名） |
| `/join-channel` | 將頻道加入現有的翻譯群組 | `group`（必填）: 目標群組名稱<br>`language`（必填）: BCP-47 語言代碼<br>`channel`（選填）: 目標頻道（預設當前頻道） |
| `/leave-channel` | 從翻譯群組中移除頻道 | `group`（必填）: 群組名稱<br>`channel`（選填）: 目標頻道（預設當前頻道） |
| `/delete-group` | 徹底刪除一個翻譯群組 | `group`（必填）: 要刪除的群組名稱 |
| `/list-groups` | 列出伺服器中的所有翻譯群組與頻道 | 無 |
| `/set-style` | 設定翻譯群組的語言風格或語氣 | `group`（必填）: 群組名稱<br>`preset`（選填）: 預設風格（見下表）<br>`custom`（選填）: 自然語言自訂指示（最多200字） |
| `/add-glossary` | 在伺服器術語表中註冊專有詞彙譯法 | `term`（必填）: 原文詞彙<br>`translation`（必填）: 指定譯法<br>`attribute`（選填）: 詞彙類別（人名、地名、流行語等）<br>`always_include`（選填）: 即使未匹配關鍵字亦常駐提示詞（預設: `false`） |
| `/list-glossary` | 檢視伺服器已註冊的術語表 | 無 |
| `/remove-glossary`| 刪除已註冊的術語表項目 | `term`（必填）: 要刪除的詞彙 |
| `/edit-forum-tags` | 編輯論壇/媒體頻道的標籤對應關係 | `group`（必填）: 群組名稱<br>`channel`（選填）: 目標論壇頻道 |
| `/bot-whitelist` | 管理允許自動翻譯的機器人與 Webhook 白名單 | 子指令: `add`, `remove`, `list`<br>`source_type`: `bot` 或 `webhook`<br>`source_id`: 機器人使用者 ID 或 Webhook ID |

#### 訊息應用程式指令（一般成員可用）
- **`View Original`（操作選單）**
  右鍵點擊或長按任一訊息 → **「應用程式」 → 「View Original」**，即可取得該訊息的原文連結與內容摘要。

---

### 進階自訂設定

#### 1. 翻譯風格設定 (`/set-style`)
根據伺服器的社群氛圍自訂翻譯的語氣風格（預設風格與自訂指示互斥）：

| 預設風格 | 特色與適用情境 |
|---|---|
| `default` | 母語者日常聊天時的自然口語表達 |
| `casual` | 朋友之間輕鬆親切的閒聊口吻 |
| `gaming` | 遊戲社群常用用語與風格 |
| `friendly` | 熱情、有禮且親切的語氣 |
| `business` | 適用於商務溝通的簡練專業表達 |
| `formal` | 使用敬語與規範用詞的正式文體 |
| `netslang` | 網路流行語與討論區風格 |
| `tweet` | 類似社群平台（X / Twitter）的短小精悍推文風格 |
| `literal` | 存在多種譯法時優先選擇字面直譯 |

#### 2. 伺服器術語表 (`/add-glossary`)
為人名、遊戲術語、專有名詞或社群流行語固定譯法，防止 AI 誤譯（每個伺服器最多支援 50 筆）：
- **詞彙屬性（`attribute`）**: 標註「人名」、「地名」、「流行語」、「縮寫」、「專業術語」等類別，有助於大模型準確理解語境。
- **常駐生效（`always_include`）**: 設為 `true` 時，即使訊息內文中未顯式包含該詞，亦會始終作為上下文提供給模型。

#### 3. 論壇與媒體頻道標籤對應 (`/edit-forum-tags`)
連結論壇或媒體頻道時，可以跨語言對應對應的標籤 ID。在某語言頻道帶標籤發文時，鏡像頻道會自動附帶對應的標籤。

#### 4. 自動訊息白名單 (`/bot-whitelist`)
預設情況下，為避免無限迴圈，機器人會自動忽略來自其他 Bot 或 Webhook 的訊息。使用 `/bot-whitelist add` 可以顯式允許指定的公告 Bot、RSS 訂閱或自動化系統訊息進行翻譯與鏡像。

---

## 2. 開發者與自建部署指南

### 環境需求與技術架構

- **開發語言**: Go 1.24 或更高版本
- **資料庫**: SQLite（純 Go 實作，基於 `modernc.org/sqlite`，無需 CGO）
- **Discord SDK**: `github.com/bwmarrin/discordgo`
- **翻譯引擎**: 相容 OpenAI 的 Chat Completions API（OpenAI, OpenRouter, Azure OpenAI, 本地模型等）
- **跨平台編譯**: 支援使用 `CGO_ENABLED=0` 輕鬆建置跨 Linux、Windows、macOS 的獨立單一執行檔。

---

### 自建部署步驟

#### 1. 建立 Discord 機器人
1. 前往 [Discord Developer Portal](https://discord.com/developers/applications) 建立新應用程式。
2. 在 **Bot** 標籤頁下啟用 `MESSAGE CONTENT INTENT`，並取得 Bot Token。
3. 在 **OAuth2 → URL Generator** 中勾選 `bot` 與 `applications.commands` 範圍及所需權限，產生連結並將機器人邀請至目標伺服器。

#### 2. 準備相容 OpenAI 的 API
取得大型語言模型服務商（如 OpenAI、OpenRouter 等）的 API 端點 URL、API Key 以及目標模型 ID。

#### 3. 設定環境變數
複製 `.env.example` 為 `.env` 並填入相關設定：

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

#### 4. 編譯與執行

本地直接執行:
```sh
go run ./cmd/discord-auto-translator
```

編譯獨立執行檔並執行:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**模型連線與協定預檢 (`--model-prewarm`)**:
在部署流程中，可使用此參數驗證 API 憑證、模型連通性與結構化輸出協定是否符合規範（不會啟動 Discord 或 HTTP 服務）：
```sh
./discord-auto-translator --model-prewarm
```

---

### 環境變數參考

| 變數名稱 | 必填 | 預設值 | 說明 |
|---|---|---|---|
| `DISCORD_TOKEN` | **是** | - | Discord 機器人權杖 |
| `OPENAI_BASE_URL` | **是** | - | 相容 OpenAI 的 Chat Completions 基礎 URL（例如 `https://api.openai.com/v1`） |
| `OPENAI_API_KEY` | **是** | - | API Bearer 驗證金鑰 |
| `OPENAI_MODEL` | **是** | - | 服務商支援的模型識別碼 |
| `OPENAI_REASONING_EFFORT` | 否 | (未設定) | 可選的 `reasoning_effort` 參數。混合推理模型若需關閉思考 Token 以降低延遲，可設為 `none` |
| `DB_PATH` | 否 | `./translator.db` | SQLite 資料庫檔案的儲存路徑 |
| `HTTP_ADDR` | 否 | `:8080` | 頭像徽章 HTTP 服務的監聽位址 |
| `PUBLIC_BASE_URL` | 否 | (未設定) | 頭像圓環徽章的公開 URL。設定後會在鏡像頭像邊緣加上最高身分組顏色的圓環 |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 否 | `100000` | 單一伺服器每分鐘允許消耗的翻譯 Token 上限 |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 否 | `120` | `/avatar` 端點單一 IP 每分鐘請求速率限制 |
| `MESSAGE_LINK_RETENTION_DAYS` | 否 | `0` | 訊息對應資料的保留天數。`0` 表示永久保留；設定天數後每 24 小時自動清理過期紀錄 |
| `GUILD_DATA_RETENTION_DAYS` | 否 | `0` | 機器人離開伺服器後其資料的保留天數。期限內重新加入會自動取消刪除排程 |

---

### 架構設計與核心機制

#### 1. 翻譯管線
1. **語境組裝 (Context Assembly)**: 收集頻道主題、近期對話上下文片段、回覆引用鏈、網頁 OGP 摘要以及縮小後的圖片附件。
2. **預留位置遮蔽 (Placeholder Masking)**: 將提及（`<@id>`）、自訂表情（`<:name:id>`）、頻道連結（`<#id>`）、URL 及程式碼區塊臨時替換為 `[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]` 等標記，防止誤譯與提示詞注入。
3. **提示詞編排與快取最佳化**: 將系統提示詞、穩定語境、歷史語境與變動內容分層排列，最大化利用大模型服務商的前綴提示詞快取（Prefix Prompt Caching）。
4. **Structured Outputs 一次性輸出**: 採用 `response_format.type=json_schema`（`strict: true`），單次請求即返回所有目標語言的結構化 JSON 結果。
5. **後處理與並發分發**: 還原預留位置，自動重寫內部 Discord 頻道與訊息連結，替換 `hreflang` 對應網址，隨後透過 Webhook 並發傳送至各目標頻道。

#### 2. 安全性與 Fail-Closed 原則
- **提示詞注入防護**: 所有使用者輸入均經過 XML 跳脫並在隔離標記中包裹，預留位置機制杜絕系統指令被惡意覆寫。
- **Fail-Closed 故障安全**: 當超出 Token 限制（`finish_reason=length`）、返回格式錯誤或發生不可恢復的連線異常時，機器人堅決不推送損壞或截斷的翻譯，而是在來源頻道發送在地化錯誤通知。

#### 3. 資料一致性與可靠性
- **等冪性保證 (Idempotency)**: 基於 `message_links` 與 `processed_events` 防止網路重試導致的訊息重複鏡像。
- **補償交易機制**: 若 Webhook 傳送成功但本地資料庫儲存失敗，會自動刪除已發送的 Discord 訊息，避免孤兒訊息殘留。
- **雙向聯動同步**: 表情回應與訊息置頂基於對應關係實現雙向同步，在任何語言頻道的變動皆會廣播至全群組。

---

### 開發與測試

#### 執行測試
```sh
go test ./...
```

#### 多語言介面文字 (i18n)
所有面向使用者的訊息與提示均在 `internal/translatorbot/ui_strings.go` 中統一管理，涵蓋 13 種語言。新增詞條必須補齊全部支援語言，並透過 `TestUIStringCatalogIsComplete` 測試驗證。

---

## 3. 開源授權

本專案基於 [MIT License](LICENSE) 授權開源。
