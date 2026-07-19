# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

一个让说不同语言的用户能在同一个 Discord 服务器中一起聊天的机器人。

为每种语言准备一个频道，并将它们关联为一个**翻译组**。当某个频道有新消息时，`google.gemma-4-26b-a4b`（经 Amazon Bedrock）会立即翻译并将其镜像到组内所有其他频道 — 保留原发送者的名字和头像 — 让每个频道读起来都像是用本语言进行的自然对话。

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

## 功能特性

- **全面同步** — 不仅是新消息：编辑、删除、回复、转发消息、表情回应、置顶、子区（文字 / 论坛 / 媒体频道）以及仅含附件的消息，都会在整个组内镜像同步。
- **消息如同本人发送** — 镜像消息通过 Webhook 发送，显示原作者的名字和头像。
- **自然的翻译** — Gemma 4 26B-A4B 会参考频道名称、主题和最近的对话历史作为上下文；每个服务器还可配置术语表，为人名和专业术语指定固定译法。
- **智能链接处理** — 指向受管理频道或消息的链接和提及会被改写为各语言频道的对应目标；带有 `hreflang` 备选版本的 URL 会替换为目标语言版本。
- **高效且安全** — 没有可翻译文本的消息（仅含 URL、提及、自定义表情、代码）会直接镜像而不调用翻译 API；每个服务器有令牌速率限制；URL、提及和代码块受到提示词注入防护。翻译失败时采用 fail-closed（不镜像，在源频道发送本地化通知）。
- **本地化界面** — 命令响应跟随用户的 Discord 客户端语言，频道通知使用频道配置的语言（支持 13 种语言，未支持语言回退到英语）。

## 前置要求

- Go 1.24 或更高版本
- 已启用 `MESSAGE CONTENT` 特权 Intent 的 Discord 机器人账号
- 可使用 Amazon Bedrock 的 AWS 账户，以及允许在 `your-aws-bedrock-region` 的 Mantle Project `your-aws-bedrock-project-id` 中创建推理的 IAM 访问密钥
- Amazon Bedrock ID

## 安装配置

### 1. 准备 Discord 机器人

1. 在 [Discord Developer Portal](https://discord.com/developers/applications) 创建应用
2. 在 **Bot** 页面：
   - 启用 `MESSAGE CONTENT INTENT`（必需）
   - 复制机器人令牌
3. 通过 **OAuth2 → URL Generator** 邀请机器人加入服务器：
   - Scopes: `bot`, `applications.commands`
   - Permissions（按 Developer Portal 中的名称）：
     - **常规**: `View Channel`, `Read Message History`
     - **消息**: `Send Messages`, `Send Messages in Threads`
     - **管理**: `Pin Messages`
     - **Webhook**: `Manage Webhooks`
     - **子区**: `Create Public Threads`, `Manage Threads`
     - **表情回应**: `Add Reactions`
   - 上述权限的整数值为 `2252126768139328`
   - 若还需同步来自其他服务器的自定义表情回应，请额外授予 `Use External Emojis`，此时权限整数值为 `2252126768401472`

### 2. 配置 Amazon Bedrock

在 `your-aws-bedrock-region` 的 Amazon Bedrock 中启用 `google.gemma-4-26b-a4b`。创建仅对该模型拥有 `bedrock-mantle:CreateInference` 权限的 IAM 用户，并在 `.env` 中设置 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`、`AWS_BEDROCK_REGION` 和 `AWS_BEDROCK_PROJECT_ID`。模型、30 秒超时和 4096 token 上限固定在代码中；区域和 Project ID 是必需的本地部署设置。

### 3. 配置环境变量

```sh
cp .env.example .env
```

编辑 `.env` 并设置以下内容：

```env
DISCORD_TOKEN=your-discord-bot-token
AWS_ACCESS_KEY_ID=your-aws-access-key-id
AWS_SECRET_ACCESS_KEY=your-aws-secret-access-key
AWS_BEDROCK_REGION=your-aws-bedrock-region
AWS_BEDROCK_PROJECT_ID=your-aws-bedrock-project-id
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

| 变量 | 必需 | 说明 |
|---|---|---|
| `DISCORD_TOKEN` | 是 | Discord 机器人令牌 |
| `AWS_ACCESS_KEY_ID` | Yes | Access key ID for the dedicated Bedrock IAM user |
| `AWS_SECRET_ACCESS_KEY` | Yes | Secret access key for the dedicated Bedrock IAM user |
| `AWS_BEDROCK_REGION` | Yes | Bedrock Mantle region, such as `your-aws-bedrock-region` |
| `AWS_BEDROCK_PROJECT_ID` | Yes | Bedrock Mantle Project ID, such as `your-aws-bedrock-project-id` |
| `DB_PATH` | 否 | SQLite 文件路径（默认: `./translator.db`） |
| `HTTP_ADDR` | 否 | 头像徽章服务器地址（默认: `:8080`） |
| `PUBLIC_BASE_URL` | 否 | 头像环徽章的公开基础 URL。未设置时，镜像消息使用 Discord 原始头像 URL，不会使用徽章服务器 |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 否 | 每个服务器每分钟的 Gemma 4 26B-A4B 令牌上限（默认: `100000`） |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 否 | `/avatar` 徽章端点的每 IP 每分钟请求上限（默认: `120`） |
| `MESSAGE_LINK_RETENTION_DAYS` | 否 | SQLite 中 `message_links` 的自动清理前保留天数。`0`（默认）禁用清理；例如 `60` 会在启动时及每 24 小时删除超过 60 天的链接 |
| `GUILD_DATA_RETENTION_DAYS` | 否 | 机器人从服务器移除后，该服务器 SQLite 数据的保留天数。`0`（默认）禁用清理；例如 `30` 会在启动时及每 24 小时删除已移除超过 30 天的服务器数据。到期前重新加入会取消计划删除 |

### Amazon Bedrock 运营约定

翻译使用 `your-aws-bedrock-region` 中 `google.gemma-4-26b-a4b` 的非流式 Mantle Responses API，并通过 `OpenAI-Project` 标头将所有请求分配给 Project `your-aws-bedrock-project-id`，固定 **30 秒**超时、**provider-default temperature 1.0**、**max_output_tokens 4096** 和由 schema 指引并由 Bot 严格校验的 JSON。所有语言在一次请求中生成。4K 上限、异常停止或无效 JSON 会使整体 fail-closed；没有重试、拆分或 fallback。Bot 不记录 prompt、响应或凭据。GCE 部署在替换前使用五分钟期限的 `--bedrock-prewarm` 验证凭据、模型访问权限和响应契约。

### 4. 启动

```sh
go run ./cmd/discord-auto-translator
```

或先构建再运行：

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## 使用方法

机器人启动后，斜杠命令会在各服务器中注册。

### 设置频道

#### 创建翻译组

在日语频道中运行 `/new-channel` 创建翻译组：

```
/new-channel language:ja
```

#### 添加其他语言频道

在英语频道中运行 `/join-channel` 将其加入该组：

```
/join-channel group:general language:en
```

如需再添加中文频道：

```
/join-channel group:general language:zh-CN
```

这样 `#general-ja`、`#general-en`、`#general-zh` 就关联在一起了。

### 命令一览

默认情况下，管理用斜杠命令仅**服务器管理员**可以执行。若要允许其他身份组使用，请在 Discord 的「服务器设置」→「整合」→ 该机器人的「管理」→「命令权限」中，进行全局或按命令的授权设置。机器人不会自行更改身份组或命令权限。

| 命令 | 说明 |
|---|---|
| `/new-channel language:[语言] channel:<频道> group:<组>` | 新建翻译组。省略 `channel` 时使用当前频道；省略 `group` 时使用频道名称 |
| `/join-channel group:[组] language:[语言] channel:<频道>` | 向组中添加频道。省略 `channel` 时使用当前频道 |
| `/leave-channel group:[组] channel:<频道>` | 使频道退出组。省略 `channel` 时使用当前频道 |
| `/delete-group group:[组]` | 删除整个组 |
| `/list-groups` | 列出此服务器的翻译组及其频道 |
| `/add-glossary term:[术语] translation:[译文] attribute:<属性> always_include:<布尔>` | 向服务器术语表注册优先译法。`attribute` 为带候选的自由输入；`always_include` 默认为 `false` |
| `/list-glossary` | 列出服务器的术语表 |
| `/remove-glossary term:[术语]` | 删除术语表条目 |
| `/set-style group:[组] preset:<预设> custom:<自定义指示>` | 设置组的翻译风格。指定 `preset` 或 `custom` 之一，不可同时指定 |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | 允许此服务器中的自动消息来源。`source_type:bot` 时，`source_id` 是机器人用户 ID；`source_type:webhook` 时，它是 Webhook ID |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | 从此服务器的允许列表中删除匹配的自动消息来源 |
| `/bot-whitelist list` | 列出此服务器中允许的机器人和 Webhook 来源 |

- 来源允许列表会持久化到 SQLite，并按每个 Discord 服务器（公会）隔离。翻译机器人管理的输出 Webhook 和翻译机器人自身的消息即使添加了相应 ID，也仍会被排除

- `language` 使用 BCP-47 格式（如 `en`、`ja`、`zh-CN`、`pt-BR`、`ko`、`fr`）
- 每个服务器最多可注册 50 条术语
- `attribute` 会提示「人名」「地名」「俚语」「缩写」「专业术语」等候选，也可自由输入任意属性。指定的属性将作为 Gemma 4 26B-A4B 判断术语含义的上下文
- 普通术语仅当待翻译正文中包含 `term`（不区分大小写）时才会加入系统指令；`always_include:true` 的术语则始终加入
- 省略 `channel` 选项时，命令作用于执行命令的频道
- 支持的频道类型：文字、公告、论坛、媒体

## 测试

```sh
go test ./...
```

## 部署到 GCE

`deploy/deploy-gce.ps1` 中包含用于 Google Compute Engine 的部署脚本（Windows PowerShell）。

从示例创建 `deploy/deploy.json` 以配置 GCE 连接。应用设置和密钥默认使用 `.env`；可通过 `deploy.json` 中的 `envFile` 或 `-EnvFile` 指定其他文件。

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# 编辑 deploy.json 和 .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # 首次设置
.\deploy\deploy-gce.ps1                          # 仅更新代码
.\deploy\deploy-gce.ps1 -UploadEnv               # 更新密钥
```

## 许可证

本项目的许可证请参阅 [LICENSE](LICENSE) 文件。
