# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

異なる言語を話すユーザーが、同じ Discord サーバー内で一緒に会話できるようにするボットです。

言語ごとに 1 チャンネルを用意して**翻訳グループ**として連携させると、あるチャンネルに投稿されたメッセージが Amazon Bedrock の `google.gemma-4-26b-a4b` で翻訳され、グループ内の他のすべてのチャンネルへ、投稿者本人の名前とアバターのままミラーリングされます。各チャンネルは、それぞれの言語での自然な会話として読めます。

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

## 特徴

- **すべてが同期される** — 新規メッセージだけでなく、編集・削除・リプライ・転送メッセージ・リアクション・ピン留め・スレッド（テキスト / フォーラム / メディア）・添付ファイルのみのメッセージも、グループ全体にミラーリングされます。
- **本人が投稿したように見える** — ミラーメッセージはウェブフック経由で、元の投稿者の名前とアバターで送信されます。
- **自然な翻訳** — Gemma 4 26B-A4B はチャンネル名・トピック・直近の会話履歴を文脈として参照します。サーバー単位の用語集で、人名や専門用語の訳語を固定することもできます。
- **リンクの賢い扱い** — 管理対象のチャンネルやメッセージへのリンク・メンションは各言語の対応先に書き換えられ、`hreflang` 代替版のある URL は対象言語版に差し替えられます。
- **効率的で安全** — 翻訳すべきテキストがない本文（URL・メンション・カスタム絵文字・コードのみ）は翻訳 API を使わずにミラーリングされ、サーバーごとのトークンレート制限が適用されます。URL・メンション・コードブロックはプロンプトインジェクションから保護されます。翻訳失敗時は fail-closed（ミラーリングせず、投稿元チャンネルへローカライズ通知）。
- **多言語 UI** — コマンド応答は実行者の Discord クライアント言語、チャンネル通知はチャンネルの登録言語で表示されます（13 言語対応・未対応言語は英語）。

## 必要なもの

- Go 1.24 以上
- Discord ボットアカウント（`MESSAGE CONTENT` 特権インテントを有効化済み）
- Amazon Bedrock を利用できる AWS アカウント
- `your-aws-bedrock-region` のBedrock Mantle Project `your-aws-bedrock-project-id` で推論作成を許可した IAM アクセスキー

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

### 2. Amazon Bedrock の設定

1. AWSコンソールを `your-aws-bedrock-region` に切り替え、管理者権限で Amazon Bedrock のModel catalogから `google.gemma-4-26b-a4b` を開き、Playgroundで一度実行します。現在のBedrockは対応モデルを既定で利用可能にし、第三者モデルで必要なMarketplace契約を初回呼び出し時に自動処理するため、独立した「有効化」ボタンがない場合があります。契約処理には最大15分ほどかかることがあります。
2. Bot専用 IAM ユーザーを作成し、AWS管理ポリシー `AmazonBedrockMantleInferenceAccess` を付与します。動作確認後、`arn:aws:bedrock-mantle:your-aws-bedrock-region:<account-id>:project/your-aws-bedrock-project-id` に限定したカスタムポリシーへ絞り込みます。
3. そのユーザーのアクセスキーを作成し、`.env` に `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`、`AWS_BEDROCK_REGION`、`AWS_BEDROCK_PROJECT_ID` を設定します。rootユーザーのアクセスキーは使用しません。

モデル・タイムアウト・出力上限はコード固定です。リージョンとProject IDはデプロイ先ごとの必須設定です。任意で `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN`（デフォルト `100000`）によりギルドごとのトークン上限を調整できます。

### 3. 環境変数の設定

```sh
cp .env.example .env
```

`.env` を編集して以下を設定します：

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

| 変数 | 必須 | 説明 |
|---|---|---|
| `DISCORD_TOKEN` | 必須 | Discord ボットトークン |
| `AWS_ACCESS_KEY_ID` | 必須 | Bedrock 専用 IAM ユーザーのアクセスキー ID |
| `AWS_SECRET_ACCESS_KEY` | 必須 | Bedrock 専用 IAM ユーザーのシークレットアクセスキー |
| `AWS_BEDROCK_REGION` | 必須 | Bedrock Mantleリージョン（例: `your-aws-bedrock-region`） |
| `AWS_BEDROCK_PROJECT_ID` | 必須 | Bedrock Mantle Project ID（例: `your-aws-bedrock-project-id`） |
| `DB_PATH` | 任意 | SQLite ファイルのパス（デフォルト: `./translator.db`） |
| `HTTP_ADDR` | 任意 | アバターバッジサーバーのアドレス（デフォルト: `:8080`） |
| `PUBLIC_BASE_URL` | 任意 | アバターリングバッジ用の公開ベース URL。未設定時は Discord の元アバター URL をそのまま使い、バッジサーバーは参照されません |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 任意 | ギルドごとの Gemma 4 26B-A4B トークン上限/分（デフォルト: `100000`） |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 任意 | `/avatar` バッジエンドポイントの IP ごとのリクエスト上限/分（デフォルト: `120`） |
| `MESSAGE_LINK_RETENTION_DAYS` | 任意 | SQLite の `message_links` を保持する日数。`0`（デフォルト）で自動削除を無効。例: `60` で 60 日より古いリンクを起動時および 24 時間ごとに削除 |
| `GUILD_DATA_RETENTION_DAYS` | 任意 | Bot がギルドから削除された後、そのギルドの SQLite データを保持する日数。`0`（デフォルト）で自動削除を無効。例: `30` で削除から 30 日を超えたギルドのデータを起動時および 24 時間ごとに削除。期限前に再参加すると削除予定を取り消す |

### Amazon Bedrock 運用契約

翻訳は `AWS_BEDROCK_REGION` の `google.gemma-4-26b-a4b` を非ストリーミング Mantle Responses API で実行します。すべてのリクエストは `OpenAI-Project` ヘッダーで `AWS_BEDROCK_PROJECT_ID` に割り当てます。固定パラメータは実行時タイムアウト **30 秒**、**provider-default temperature 1.0**、**max_output_tokens 4096**、`store=false` です。Gemma 4はBedrock Structured Outputs非対応のため、固定JSON Schemaをsystem instructionへ含め、返された複数言語の `translations` 配列をBotが厳密検証します。

対象言語すべてを1回のリクエストで生成します。4K出力上限への到達、正常以外の終了理由、不正JSON、言語タグの欠落・順序違い、空の翻訳、余分なフィールドは全体失敗です。retry、リクエスト分割、別プロバイダーへのfallback、互換経路はありません。

ボットはプロンプト・応答・認証情報・プロバイダーのエラーメッセージをログに出しません。Mantleはリクエスト単位のBedrock metadata非対応なので、Discord IDをmetadataとして送信しません。安全な診断情報はエラーtype、code、param、request IDだけです。翻訳失敗とレート制限超過は **fail-closed** — メッセージはミラーリングされず、投稿元チャンネルにローカライズ通知が送られます。

GCEデプロイスクリプトは稼働中バイナリを置換する前に、5分期限の `--bedrock-prewarm` で認証情報・モデルアクセス・レスポンス契約を検証します。通常サービスの呼び出し期限は30秒です。

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
| `/new-channel language:[言語] channel:<チャンネル> group:<グループ>` | 翻訳グループを新規作成。`channel` を省略すると実行したチャンネル、`group` を省略するとチャンネル名が使われます |
| `/join-channel group:[グループ] language:[言語] channel:<チャンネル>` | グループにチャンネルを追加。`channel` を省略すると実行したチャンネルが対象になります |
| `/leave-channel group:[グループ] channel:<チャンネル>` | グループからチャンネルを離脱。`channel` を省略すると実行したチャンネルが対象になります |
| `/delete-group group:[グループ]` | グループ全体を削除 |
| `/list-groups` | このサーバーの翻訳グループとチャンネルを一覧表示 |
| `/add-glossary term:[用語] translation:[訳] attribute:<属性> always_include:<常時使用>` | サーバー用語集に優先訳を登録。`attribute` は候補付き自由入力です。`always_include` の既定値は `false` です |
| `/list-glossary` | サーバーの用語集を一覧表示 |
| `/remove-glossary term:[用語]` | 用語集エントリを削除 |
| `/set-style group:[グループ] preset:<プリセット> custom:<カスタム指示>` | グループの翻訳スタイルを設定。`preset` か `custom` のどちらか一方を指定してください |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | このサーバーで自動送信元を許可。`source_type:bot` の `source_id` は Bot ユーザー ID、`source_type:webhook` では Webhook ID です |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | このサーバーの許可リストから一致する自動送信元を削除 |
| `/bot-whitelist list` | このサーバーで許可されている Bot と Webhook の送信元を一覧表示 |

- 送信元許可リストは SQLite に永続化され、Discord サーバー（ギルド）ごとに分離されます。翻訳 Bot が管理する出力 Webhook と翻訳 Bot 自身のメッセージは、ID を追加しても引き続き除外されます

- `language` は BCP-47 形式（`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr` など）
- 用語集はサーバーごとに最大 50 件まで登録できます
- `attribute` には「人名」「地名」「スラング」「略語」「専門用語」が候補表示され、任意の属性も自由入力できます。指定した属性は Gemma 4 26B-A4B が用語の意味を判断する文脈として使われます
- 通常の用語は翻訳対象本文に `term` が大文字・小文字を無視して含まれる場合だけシステム指示に追加されます。`always_include:true` の用語は常に追加されます
- `channel` オプションを省略すると、コマンドを実行したチャンネルが対象になります
- 対応チャンネルタイプ: テキスト、ニュース、フォーラム、メディア

## テスト

```sh
go test ./...
```

## GCE へのデプロイ

Google Compute Engine へのデプロイスクリプトが `deploy/deploy-gce.ps1` に含まれています（Windows PowerShell 用）。

`deploy/deploy.json.example` から `deploy/deploy.json` を作成し、GCE 接続先を設定します。アプリ設定とシークレットはデフォルトで `.env` を使います。別ファイルを使う場合は `deploy.json` の `envFile` または `-EnvFile` で指定できます。

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# deploy.json と .env を編集

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # 初回セットアップ
.\deploy\deploy-gce.ps1                          # コード更新のみ
.\deploy\deploy-gce.ps1 -UploadEnv               # シークレット更新
```

## ライセンス

このプロジェクトのライセンスについては [LICENSE](LICENSE) ファイルを参照してください。
