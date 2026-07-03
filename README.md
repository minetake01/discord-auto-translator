# Discord Auto Translator

Discord のチャンネル間でメッセージを自動翻訳・ミラーリングするボットです。  
Google Gemini を使って、異なる言語を話すユーザーが同じサーバー内でシームレスに会話できるようにします。

## 特徴

- **リアルタイム翻訳**: メッセージが投稿された瞬間に翻訳して別チャンネルへ転送
- **送信者の偽装**: ウェブフックを使い、投稿者の名前とアバターをそのまま表示
- **編集・削除の同期**: 元メッセージを編集・削除すると翻訳版も更新・削除
- **リプライの同期**: 返信引用も翻訳されてリンク付きで表示
- **リアクションの同期**: 絵文字リアクションが全言語チャンネルに反映
- **スレッドの同期**: スレッド作成・名前変更・削除も対応（テキスト・フォーラム・メディア）
- **用語集**: サーバー単位で優先翻訳を登録し、翻訳品質を調整
- **添付ファイルの共有**: 本文が空でも、署名なし Discord CDN URL を使って添付ファイルのみのメッセージをミラーリング
- **翻訳文脈の考慮**: チャンネル名・トピック・直近の会話履歴を踏まえて自然な翻訳
- **プロンプトインジェクション対策**: URL・メンション・コードブロック等を保護

## 必要なもの

- Go 1.24 以上
- Discord ボットアカウント（`MESSAGE CONTENT` 特権インテントを有効化済み）
- Google Gemini API キー

## セットアップ

### 1. Discord ボットの準備

1. [Discord Developer Portal](https://discord.com/developers/applications) でアプリケーションを作成
2. **Bot** ページで:
   - `MESSAGE CONTENT INTENT` を有効化（必須）
   - ボットトークンをコピー
3. **OAuth2 → URL Generator** でボットをサーバーに招待:
   - Scopes: `bot`, `applications.commands`
   - Permissions（Developer Portal の表示名）:
     - **基本**: `View Channel`, `Read Message History`
     - **メッセージ**: `Send Messages`, `Send Messages in Threads`
     - **モデレーション**: `Pin Messages`
     - **ウェブフック**: `Manage Webhooks`
     - **スレッド**: `Create Public Threads`, `Manage Threads`
     - **リアクション**: `Add Reactions`
   - 上記の permissions 整数は `2252126768139328` です
   - 外部サーバー由来のカスタム絵文字リアクションも同期する場合は、追加で `Use External Emojis` を許可してください。その場合の permissions 整数は `2252126768401472` です

### 2. Gemini API キーの取得

[Google AI Studio](https://aistudio.google.com/) で API キーを取得してください。

### 3. 環境変数の設定

```sh
cp .env.example .env
```

`.env` を編集して以下を設定します：

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
GEMINI_RATE_LIMIT_TOKENS_PER_MIN=100000
```

| 変数 | 必須 | 説明 |
|---|---|---|
| `DISCORD_TOKEN` | 必須 | Discord ボットトークン |
| `GEMINI_API_KEY` | 必須 | Gemini API キー |
| `DB_PATH` | 任意 | SQLite ファイルのパス（デフォルト: `./translator.db`） |
| `HTTP_ADDR` | 任意 | アバターバッジサーバーのアドレス（デフォルト: `:8080`） |
| `PUBLIC_BASE_URL` | 任意 | アバターにオレンジリングバッジを付ける場合のベース URL |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | 任意 | ギルドごとの Gemini トークン上限/分（デフォルト: `100000`） |

### 4. 起動

```sh
go run ./cmd/discord-auto-translator
```

または、ビルドして実行：

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## 使い方

ボットを起動するとスラッシュコマンドが各サーバーに登録されます。

### チャンネルの設定

#### 翻訳グループを作成する

日本語チャンネルで `/new-channel` を実行して翻訳グループを作成します：

```
/new-channel language:ja
```

#### 他の言語チャンネルを追加する

英語チャンネルで `/join-channel` を実行してグループに参加させます：

```
/join-channel group:general language:en
```

中国語チャンネルも追加する場合：

```
/join-channel group:general language:zh-CN
```

これで `#general-ja`, `#general-en`, `#general-zh` が連携されます。

### コマンド一覧

管理用スラッシュコマンドは、デフォルトでは**サーバー管理者**のみが実行できます。追加のロールに実行を許可する場合は、Discord の「サーバー設定」→「連携サービス」→対象 Bot の「管理」→「コマンド権限」で、全コマンド共通またはコマンド単位の許可を設定してください。Bot はロールやコマンド権限を自動変更しません。

| コマンド | 説明 |
|---|---|
| `/new-channel language:[言語]` | 翻訳グループを新規作成 |
| `/join-channel group:[グループ] language:[言語]` | グループにチャンネルを追加 |
| `/leave-channel group:[グループ]` | グループからチャンネルを離脱 |
| `/delete-group group:[グループ]` | グループ全体を削除 |
| `/add-glossary term:[用語] translation:[訳]` | サーバー用語集に優先訳を登録 |
| `/list-glossary` | サーバーの用語集を一覧表示 |
| `/remove-glossary term:[用語]` | 用語集エントリを削除 |

- `language` は BCP-47 形式（`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr` など）
- 用語集はサーバーごとに最大 10 件まで登録可能
- `channel` オプションを省略すると、コマンドを実行したチャンネルが対象
- 対応チャンネルタイプ: テキスト、ニュース、フォーラム、メディア

## テスト

```sh
go test ./...
```

## GCE へのデプロイ

Google Compute Engine へのデプロイスクリプトが `deploy/deploy-gce.ps1` に含まれています（Windows PowerShell 用）。

```powershell
# 初回セットアップ（Caddy + systemd のインストール）
.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv

# コード更新時
.\deploy\deploy-gce.ps1
```

## ライセンス

このプロジェクトのライセンスについては [LICENSE](LICENSE) ファイルを参照してください。
