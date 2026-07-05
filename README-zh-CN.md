# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

一个让说不同语言的用户能在同一个 Discord 服务器中一起聊天的机器人。

为每种语言准备一个频道，并将它们关联为一个**翻译组**。当某个频道有新消息时，Google Gemini 会立即翻译并将其镜像到组内所有其他频道 — 保留原发送者的名字和头像 — 让每个频道读起来都像是用本语言进行的自然对话。

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

## 功能特性

- **全面同步** — 不仅是新消息：编辑、删除、回复、转发消息、表情回应、置顶、子区（文字 / 论坛 / 媒体频道）以及仅含附件的消息，都会在整个组内镜像同步。
- **消息如同本人发送** — 镜像消息通过 Webhook 发送，显示原作者的名字和头像。
- **自然的翻译** — Gemini 会参考频道名称、主题和最近的对话历史作为上下文；每个服务器还可配置术语表，为人名和专业术语指定固定译法。
- **智能链接处理** — 指向受管理频道或消息的链接和提及会被改写为各语言频道的对应目标；带有 `hreflang` 备选版本的 URL 会替换为目标语言版本。
- **高效且安全** — 没有可翻译文本的消息（仅含 URL、提及、自定义表情、代码）会直接镜像而不调用 Gemini API；每个服务器有令牌速率限制；URL、提及和代码块受到提示词注入防护。
- **本地化界面** — 命令响应跟随用户的 Discord 客户端语言，频道通知使用频道配置的语言（支持 13 种语言，未支持语言回退到英语）。

## 前置要求

- Go 1.24 或更高版本
- 已启用 `MESSAGE CONTENT` 特权 Intent 的 Discord 机器人账号
- Google Gemini API 密钥

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

### 2. 获取 Gemini API 密钥

在 [Google AI Studio](https://aistudio.google.com/) 获取 API 密钥。

### 3. 配置环境变量

```sh
cp .env.example .env
```

编辑 `.env` 并设置以下内容：

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
GEMINI_RATE_LIMIT_TOKENS_PER_MIN=100000
```

| 变量 | 必需 | 说明 |
|---|---|---|
| `DISCORD_TOKEN` | 是 | Discord 机器人令牌 |
| `GEMINI_API_KEY` | 是 | Gemini API 密钥 |
| `DB_PATH` | 否 | SQLite 文件路径（默认: `./translator.db`） |
| `HTTP_ADDR` | 否 | 头像徽章服务器地址（默认: `:8080`） |
| `PUBLIC_BASE_URL` | 否 | 为头像添加橙色圆环徽章时的基础 URL |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | 否 | 每个服务器每分钟的 Gemini 令牌上限（默认: `100000`） |

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
| `/new-channel language:[语言]` | 新建翻译组 |
| `/join-channel group:[组] language:[语言]` | 向组中添加频道 |
| `/leave-channel group:[组]` | 使频道退出组 |
| `/delete-group group:[组]` | 删除整个组 |
| `/add-glossary term:[术语] translation:[译文] attribute:[属性] always_include:[布尔]` | 向服务器术语表注册优先译法（`attribute` 为带候选的自由输入，`always_include` 默认为 `false`） |
| `/list-glossary` | 列出服务器的术语表 |
| `/remove-glossary term:[术语]` | 删除术语表条目 |

- `language` 使用 BCP-47 格式（如 `en`、`ja`、`zh-CN`、`pt-BR`、`ko`、`fr`）
- 每个服务器最多可注册 50 条术语
- `attribute` 会提示「人名」「地名」「俚语」「缩写」「专业术语」等候选，也可自由输入任意属性。指定的属性将作为 Gemini 判断术语含义的上下文
- 普通术语仅当待翻译正文中包含 `term`（不区分大小写）时才会加入系统指令；`always_include:true` 的术语则始终加入
- 省略 `channel` 选项时，命令作用于执行命令的频道
- 支持的频道类型：文字、公告、论坛、媒体

## 测试

```sh
go test ./...
```

## 部署到 GCE

`deploy/deploy-gce.ps1` 中包含用于 Google Compute Engine 的部署脚本（Windows PowerShell）。

```powershell
# 首次设置（安装 Caddy + systemd）
.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv

# 更新代码时
.\deploy\deploy-gce.ps1
```

## 许可证

本项目的许可证请参阅 [LICENSE](LICENSE) 文件。
