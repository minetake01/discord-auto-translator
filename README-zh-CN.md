# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

让说不同语言的用户能够在同一个 Discord 服务器中，使用各自的母语进行实时无障碍交流的 Discord 机器人。

为每种语言准备一个频道并将其关联为一个**翻译组**后，在任意频道发送的消息都会通过兼容 OpenAI 的大语言模型（Chat Completions API）自动翻译，并以**原发言者的名字和头像**镜像推送到组内所有其他语言频道。参与者只需在自己的母语频道中阅读和发言，即可享受自然的跨语言交流。

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. 用户与服务器管理员指南

### 主要特性与用户体验

- **像平时一样自然聊天**
  无需输入任何特殊指令或前缀。像平常一样发送消息，机器人就会自动将其翻译并同步发送到其他语言频道。
- **保留原发言者身份**
  镜像消息通过 Webhook 发送，完整保留原发言者的显示昵称和头像。
- **全方位实时双向同步**
  - **新消息与附件**: 不仅支持文本，还完整支持图片（包括图片替代文本 / Alt text）及各类文件附件。
  - **消息编辑与删除**: 编辑或删除原消息时，其他语言频道的翻译消息会实时同步更新或删除。
  - **回复（引用）**: 在目标语言中附带被引用消息的摘要并生成对应跳转链接（伪引用形式）。
  - **转发消息**: 保留转发内容并对转发标题进行本地化展示。
  - **表情回应与置顶**: 对消息添加/取消表情回应（Reaction）以及置顶（Pin）操作均双向同步。
  - **子区与论坛**: 支持普通子区（Thread）以及论坛（Forum）和媒体频道，包括论坛标签（Tag）的自动映射。
  - **投票（Polls）**: 投票消息会自动翻译问题与选项，以 Embed 形式镜像展示，并在投票结束时通知最终结果。
- **“View Original”（查看原文）功能**
  右键（手机端长按）任意翻译消息，选择 **“应用 (Apps)” → “View Original”**，即可查看原消息的跳转链接和原文预览（仅自己可见）。
- **智能链接与媒体转换**
  - 消息中指向组内其他频道、消息或子区的链接与提及，会自动改写为目标语言频道的对应 ID。
  - 指向外部网站的 URL 若包含 `hreflang` 多语言版本，会自动替换为目标语言对应的 URL。

---

### 服务器接入步骤

#### 1. 邀请机器人到服务器
在 Discord 开发者后台（Developer Portal）生成邀请链接并赋予以下权限：

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **机器人权限（Bot Permissions）**:
  - **通用**: `View Channels`（查看频道）, `Read Message History`（读取消息历史）
  - **文字**: `Send Messages`（发送消息）, `Send Messages in Threads`（在子区中发送消息）
  - **管理**: `Pin Messages`（置顶消息）
  - **Webhook**: `Manage Webhooks`（管理 Webhook）
  - **子区**: `Create Public Threads`（创建公共子区）, `Manage Threads`（管理子区）
  - **表情**: `Add Reactions`（添加表情回应）
- **权限数值 (Permissions Integer)**: `2252126768139328`
  - *注：若需要同步来自其他外部服务器的自定义表情，请勾选 `Use External Emojis`（权限数值: `2252126768401472`）。*

#### 2. 开启特权 Intent
确保在 Discord 开发者后台的 **Bot** 页面中开启了 `MESSAGE CONTENT INTENT`（消息内容特权 Intent）。

---

### 频道配置（基本操作）

#### 创建翻译组
在日语频道（例如 `#general-ja`）运行 `/new-channel` 创建翻译组：

```
/new-channel language:ja
```
*注：若省略 `group`，默认使用当前频道名称作为组名。*

#### 加入其他语言频道
在英语频道（例如 `#general-en`）运行 `/join-channel` 加入该组：

```
/join-channel group:general language:en
```

加入中文频道（例如 `#general-zh`）：

```
/join-channel group:general language:zh-CN
```

完成后，`#general-ja`、`#general-en` 与 `#general-zh` 将互相关联并开启自动翻译。

#### 退出频道与解散组
- 将频道移出组: `/leave-channel group:general`
- 完全解散并删除组: `/delete-group group:general`
- 查看当前服务器中的翻译组与频道配置: `/list-groups`

---

### 指令参考

#### 管理员斜杠指令
管理指令默认仅限**服务器管理员（Administrator 权限）**运行。如需授权其他身份组，可在 Discord 的“服务器设置”→“集成”→“机器人管理”→“指令权限”中配置。

| 指令 | 说明 | 主要参数 |
|---|---|---|
| `/new-channel` | 创建新的翻译组并注册频道 | `language`（必填）: BCP-47 语言代码<br>`channel`（选填）: 目标频道（默认当前频道）<br>`group`（选填）: 组标识名（默认频道名） |
| `/join-channel` | 将频道加入现有的翻译组 | `group`（必填）: 目标组名称<br>`language`（必填）: BCP-47 语言代码<br>`channel`（选填）: 目标频道（默认当前频道） |
| `/leave-channel` | 从翻译组中移除频道 | `group`（必填）: 组名称<br>`channel`（选填）: 目标频道（默认当前频道） |
| `/delete-group` | 彻底删除一个翻译组 | `group`（必填）: 要删除的组名称 |
| `/list-groups` | 列出服务器中的所有翻译组与频道 | 无 |
| `/set-style` | 设置翻译组的语言风格或语气 | `group`（必填）: 组名称<br>`preset`（选填）: 预设风格（见下表）<br>`custom`（选填）: 自然语言自定义指示（最多200字） |
| `/add-glossary` | 在服务器术语表中注册专有词汇译法 | `term`（必填）: 原文词汇<br>`translation`（必填）: 指定译法<br>`attribute`（选填）: 词汇类别（人名、地名、俚语等）<br>`always_include`（选填）: 即使未匹配到关键字也常驻提示词（默认: `false`） |
| `/list-glossary` | 查看服务器已注册的术语表 | 无 |
| `/remove-glossary`| 删除已注册的术语表条目 | `term`（必填）: 要删除的词汇 |
| `/edit-forum-tags` | 编辑论坛/媒体频道的标签对应关系 | `group`（必填）: 组名称<br>`channel`（选填）: 目标论坛频道 |
| `/bot-whitelist` | 管理允许自动翻译的机器人和 Webhook 白名单 | 子指令: `add`, `remove`, `list`<br>`source_type`: `bot` 或 `webhook`<br>`source_id`: 机器人用户 ID 或 Webhook ID |

#### 消息应用指令（普通成员可用）
- **`View Original`（上下文菜单）**
  右键或长按任意消息 → **“应用” → “View Original”**，即可获取该消息的原文链接与内容摘要。

---

### 高级自定义设置

#### 1. 翻译风格设置 (`/set-style`)
根据服务器的社区氛围定制翻译的语气风格（预设风格与自定义指示互斥）：

| 预设风格 | 特点与适用场景 |
|---|---|
| `default` | 母语者日常聊天时的自然口语表达 |
| `casual` | 朋友之间轻松亲切的闲聊口吻 |
| `gaming` | 游戏社区常用俚语与风格 |
| `friendly` | 热情、礼貌且友善的语气 |
| `business` | 适用于商务沟通的简练专业表达 |
| `formal` | 使用敬语与规范措辞的正式文体 |
| `netslang` | 网络流行语与论坛风格 |
| `tweet` | 类似社交媒体（X / Twitter）的短小精炼推文风格 |
| `literal` | 存在多种译法时优先选择字面直译 |

#### 2. 服务器术语表 (`/add-glossary`)
为人名、游戏术语、专有名词或社区黑话固定译法，防止 AI 误译（每个服务器最多支持 50 条）：
- **词汇属性（`attribute`）**: 标注“人名”、“地名”、“俚语”、“缩写”、“专业术语”等类别，有助于大模型准确理解语义。
- **常驻生效（`always_include`）**: 设为 `true` 时，即使消息正文中未显式包含该词，也会始终作为上下文提供给模型。

#### 3. 论坛与媒体频道标签映射 (`/edit-forum-tags`)
关联论坛或媒体频道时，可以跨语言映射对应的标签 ID。在某语言频道带标签发帖时，镜像频道会自动附带对应的标签。

#### 4. 自动消息白名单 (`/bot-whitelist`)
默认情况下，为防止无限循环，机器人会自动忽略来自其他 Bot 或 Webhook 的消息。使用 `/bot-whitelist add` 可以显式允许指定的公告 Bot、RSS 订阅或自动化系统消息进行翻译与镜像。

---

## 2. 开发者与自建部署指南

### 环境要求与技术栈

- **开发语言**: Go 1.24 或更高版本
- **数据库**: SQLite（纯 Go 实现，基于 `modernc.org/sqlite`，无需 CGO）
- **Discord SDK**: `github.com/bwmarrin/discordgo`
- **翻译引擎**: 兼容 OpenAI 的 Chat Completions API（OpenAI, OpenRouter, Azure OpenAI, 本地模型等）
- **跨平台编译**: 支持使用 `CGO_ENABLED=0` 轻松构建跨 Linux、Windows、macOS 的独立单文件二进制。

---

### 自建部署步骤

#### 1. 创建 Discord 机器人
1. 前往 [Discord Developer Portal](https://discord.com/developers/applications) 创建新应用。
2. 在 **Bot** 选项卡下启用 `MESSAGE CONTENT INTENT`，并获取 Bot Token。
3. 在 **OAuth2 → URL Generator** 中勾选 `bot` 和 `applications.commands` 作用域及所需权限，生成链接并将机器人邀请至目标服务器。

#### 2. 准备兼容 OpenAI 的 API
获取大模型服务商（如 OpenAI、OpenRouter 等）的 API 端点 URL、API Key 以及目标模型 ID。

#### 3. 配置环境变量
复制 `.env.example` 为 `.env` 并填写相关配置：

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

#### 4. 编译与运行

本地直接运行:
```sh
go run ./cmd/discord-auto-translator
```

编译独立二进制并运行:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**模型连接与协议预检 (`--model-prewarm`)**:
在部署流程中，可使用此参数验证 API 凭证、模型连通性与结构化输出协议是否符合要求（不会启动 Discord 或 HTTP 服务）：
```sh
./discord-auto-translator --model-prewarm
```

---

### 环境变量参考

| 变量名 | 必填 | 默认值 | 说明 |
|---|---|---|---|
| `DISCORD_TOKEN` | **是** | - | Discord 机器人令牌 |
| `OPENAI_BASE_URL` | **是** | - | 兼容 OpenAI 的 Chat Completions 基础 URL（例如 `https://api.openai.com/v1`） |
| `OPENAI_API_KEY` | **是** | - | API Bearer 认证密钥 |
| `OPENAI_MODEL` | **是** | - | 服务商支持的模型标识符 |
| `OPENAI_REASONING_EFFORT` | 否 | (未设置) | 可选的 `reasoning_effort` 参数。混合推理模型若需关闭思考 Token 以降低延迟，可设为 `none` |
| `DB_PATH` | 否 | `./translator.db` | SQLite 数据库文件的存储路径 |
| `HTTP_ADDR` | 否 | `:8080` | 头像徽章 HTTP 服务的监听地址 |
| `PUBLIC_BASE_URL` | 否 | (未设置) | 头像圆环徽章的公开 URL。设置后会在镜像头像边缘加上最高身份组颜色的圆环 |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 否 | `100000` | 单个服务器每分钟允许消耗的翻译 Token 上限 |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 否 | `120` | `/avatar` 端点单 IP 每分钟请求速率限制 |
| `MESSAGE_LINK_RETENTION_DAYS` | 否 | `0` | 消息映射数据的保留天数。`0` 表示永久保留；设置天数后每 24 小时自动清理过期记录 |
| `GUILD_DATA_RETENTION_DAYS` | 否 | `0` | 机器人离开服务器后其数据的保留天数。期限内重新加入会自动取消删除任务 |

---

### 架构设计与核心机制

#### 1. 翻译流水线
1. **上下文组装 (Context Assembly)**: 收集频道主题、近期会话上下文切片、回复引用链、网页 OGP 摘要以及缩略后的图片附件。
2. **占位符屏蔽 (Placeholder Masking)**: 将提及（`<@id>`）、自定义表情（`<:name:id>`）、频道链接（`<#id>`）、URL 及代码块临时替换为 `[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]` 等标记，防止误译与提示词注入。
3. **提示词编排与缓存优化**: 将系统提示词、稳定上下文、历史上下文与变动内容分层排列，最大化利用大模型服务商的前缀提示词缓存（Prefix Prompt Caching）。
4. **Structured Outputs 一次性输出**: 采用 `response_format.type=json_schema`（`strict: true`），单次请求即返回所有目标语言的结构化 JSON 结果。
5. **后处理与并发分发**: 还原占位符，自动重写内部 Discord 频道与消息链接，替换 `hreflang` 对应网址，随后通过 Webhook 并发投递至各目标频道。

#### 2. 安全性与 Fail-Closed 原则
- **提示词注入防护**: 所有用户输入均经过 XML 转义并在隔离标记中包裹，占位符机制杜绝系统指令被恶意覆盖。
- **Fail-Closed 故障安全**: 当超出 Token 限制（`finish_reason=length`）、返回格式错误或发生不可恢复的通信异常时，机器人坚决不推送破损或截断的翻译，而是在源频道发送本地化错误提示。

#### 3. 数据一致性与可靠性
- **幂等性保障 (Idempotency)**: 基于 `message_links` 与 `processed_events` 防止网络重试导致的消息重复镜像。
- **补偿事务机制**: 若 Webhook 投递成功但本地数据库保存失败，会自动删除已发送的 Discord 消息，避免孤儿消息残留。
- **双向联动同步**: 表情回应与消息置顶基于对应映射关系实现双向同步，在任何语言频道的变动均会广播至全组。

---

### 开发与测试

#### 运行测试
```sh
go test ./...
```

#### 多语言界面文本 (i18n)
所有面向用户的消息与提示均在 `internal/translatorbot/ui_strings.go` 中统一管理，涵盖 13 种语言。新增词条必须补齐全部支持语言，并通过 `TestUIStringCatalogIsComplete` 测试验证。

---

## 3. 开源许可证

本项目基于 [MIT License](LICENSE) 协议开源。
