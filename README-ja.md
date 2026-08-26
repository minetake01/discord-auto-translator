# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

異なる言語を話すユーザーが、同じ Discord サーバー内でそれぞれの母国語を使ってリアルタイムに会話できるようにする Discord ボットです。

言語ごとにチャンネルを用意して**翻訳グループ**として連携させると、いずれかのチャンネルに投稿されたメッセージが OpenAI 互換の LLM（Chat Completions API）によって自動翻訳され、グループ内の全チャンネルへ**送信者本人の名前とアバター**でミラーリングされます。参加者は母国語のチャンネルを見るだけで、自然な会話を楽しむことができます。

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. ユーザー & サーバー管理者向けガイド

### 主な特徴とユーザー体験

- **いつも通りチャットするだけ**
  特別なコマンドを入力することなく、普段通りメッセージを送信するだけで自動的に他言語チャンネルへ翻訳・送信されます。
- **本人が投稿したような自然な表示**
  ミラーリングされたメッセージは Webhook 経由で送信され、元の投稿者の表示名とアバター画像がそのまま引き継がれます。
- **すべての操作をリアルタイムに完全同期**
  - **新規メッセージ・添付ファイル**: テキストだけでなく、画像（代替テキスト含む）や各種添付ファイルも同期されます。
  - **メッセージの編集と削除**: 原文を編集・削除すると、連動して他言語の翻訳メッセージも即座に編集・削除されます。
  - **返信（リプライ）**: 返信元メッセージの引用スニペットを送信先言語で付与して同期します（疑似リプライ）。
  - **転送メッセージ**: 転送元の情報を保ったまま、見出しをローカライズして同期します。
  - **リアクション・ピン留め**: メッセージにつけられたリアクションやピン留め操作も双方向で同期されます。
  - **スレッド・フォーラム**: 通常スレッドのほか、フォーラムチャンネルやメディアチャンネルのスレッド・タグも同期されます。
  - **投票（Polls）**: 投票メッセージは質問と選択肢が翻訳されて Embed 形式でミラーリングされ、投票終了時には結果も通知されます。
- **「View Original」（原文の確認）機能**
  翻訳されたメッセージを右クリック（スマートフォンでは長押し）して「アプリ」→「View Original」を選択すると、元のメッセージへのジャンプリンクと原文のスニペットを自分だけに表示（エフェメラル表示）できます。
- **スマートなリンクとメディア処理**
  - 翻訳グループ内の別言語チャンネルやメッセージへのリンク・メンションは、送信先言語に対応するチャンネル・メッセージ ID へ自動的に書き換えられます。
  - 外部 Web サイトへの URL に `hreflang` による多言語版が存在する場合、自動的に送信先言語の URL に差し替えられます。

---

### サーバーへの導入手順

#### 1. Bot をサーバーに招待する
以下のリンク生成手順に従い、必要な権限を付与して Bot をサーバーに招待します。

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **必要な権限（Bot Permissions）**:
  - **一般**: `View Channels`（チャンネルを見る）, `Read Message History`（メッセージ履歴を読む）
  - **テキスト**: `Send Messages`（メッセージを送信）, `Send Messages in Threads`（スレッドでメッセージを送信）
  - **モデレーション**: `Pin Messages`（メッセージをピン留め）
  - **Webhook**: `Manage Webhooks`（ウェブフックの管理）
  - **スレッド**: `Create Public Threads`（公開スレッドの作成）, `Manage Threads`（スレッドの管理）
  - **リアクション**: `Add Reactions`（リアクションの追加）
- **権限整数**: `2252126768139328`
  - ※別サーバー由来のカスタム絵文字リアクションも同期したい場合は、追加で `Use External Emojis`（外部絵文字の使用）を許可してください（権限整数: `2252126768401472`）。

#### 2. 特権インテントの確認
Bot アカウント側で `MESSAGE CONTENT INTENT`（メッセージコンテンツ特権インテント）が有効化されている必要があります。

---

### チャンネル設定（基本操作）

#### 翻訳グループを作成する
日本語チャンネル（例: `#general-ja`）で `/new-channel` を実行し、翻訳グループを作成します：

```
/new-channel language:ja
```
*※ `group` を省略すると現在のチャンネル名がグループ名になります。*

#### 他の言語チャンネルを追加する
英語チャンネル（例: `#general-en`）で `/join-channel` を実行し、先ほど作成したグループに参加させます：

```
/join-channel group:general language:en
```

同様に中国語チャンネル（例: `#general-zh`）を追加する場合：

```
/join-channel group:general language:zh-CN
```

これで `#general-ja`、`#general-en`、`#general-zh` が連携され、相互の自動翻訳が開始されます。

#### チャンネルの離脱とグループの削除
- チャンネルをグループから外す場合: `/leave-channel group:general`
- グループ自体を完全に削除する場合: `/delete-group group:general`
- 現在のグループとチャンネルの構成を確認する場合: `/list-groups`

---

### コマンドリファレンス

#### スラッシュコマンド（管理者向け）
管理用コマンドは、デフォルトで**サーバー管理者（Administrator 権限）**のみが実行可能です。追加のロールに実行を許可したい場合は、Discord の「サーバー設定」→「連携サービス」→対象 Bot の「管理」→「コマンド権限」から設定してください。

| コマンド | 説明 | 主なオプション |
|---|---|---|
| `/new-channel` | 新しい翻訳グループを作成し、チャンネルを登録 | `language`（必須）: BCP-47 言語コード<br>`channel`（任意）: 対象チャンネル（省略時は現在のチャンネル）<br>`group`（任意）: グループ識別名（省略時はチャンネル名） |
| `/join-channel` | 既存の翻訳グループにチャンネルを追加 | `group`（必須）: 参加先グループ名<br>`language`（必須）: BCP-47 言語コード<br>`channel`（任意）: 対象チャンネル（省略時は現在のチャンネル） |
| `/leave-channel` | 翻訳グループからチャンネルを離脱 | `group`（必須）: グループ名<br>`channel`（任意）: 対象チャンネル（省略時は現在のチャンネル） |
| `/delete-group` | 翻訳グループを完全に削除 | `group`（必須）: 削除するグループ名 |
| `/list-groups` | サーバー内の翻訳グループとチャンネル一覧を表示 | なし |
| `/set-style` | 翻訳グループの文体やトーンを設定 | `group`（必須）: グループ名<br>`preset`（任意）: スタイルプリセット（下記参照）<br>`custom`（任意）: 自然言語によるカスタム指示（最大200文字） |
| `/add-glossary` | サーバー専用の用語集（優先訳）を登録 | `term`（必須）: 原文の用語<br>`translation`（必須）: 優先する訳語<br>`attribute`（任意）: 用語の属性（人名、地名、スラングなど）<br>`always_include`（任意）: 本文に含まれなくても常にプロンプトへ含めるか（既定値: `false`） |
| `/list-glossary` | 登録されている用語集一覧を表示 | なし |
| `/remove-glossary`| 登録済みの用語集エントリを削除 | `term`（必須）: 削除する用語 |
| `/edit-forum-tags` | フォーラム／メディアチャンネルのタグ対応付けを編集 | `group`（必須）: グループ名<br>`channel`（任意）: 対象フォーラムチャンネル |
| `/bot-whitelist` | 他の Bot や Webhook からのメッセージの翻訳許可を管理 | サブコマンド: `add`（追加）, `remove`（削除）, `list`（一覧）<br>`source_type`: `bot` または `webhook`<br>`source_id`: Bot ユーザー ID または Webhook ID |

#### メッセージコマンド（一般ユーザー利用可能）
- **`View Original`（アプリメニュー）**
  任意のメッセージを右クリックまたは長押し →「アプリ」→「View Original」を実行することで、翻訳元メッセージの URL と原文プレビューを取得できます。

---

### 高度なカスタマイズ

#### 1. 翻訳スタイルの設定 (`/set-style`)
グループの会話の雰囲気に合わせて、翻訳時のデフォルトの文体を指定できます（プリセットとカスタム指示は排他指定）。

| プリセット | 特徴・用途 |
|---|---|
| `default` | ネイティブがチャットで実際に使う自然な会話スタイル |
| `casual` | 友達同士のようなカジュアルで親しみやすい口調 |
| `gaming` | ゲームコミュニティ向けのカジュアルな表現 |
| `friendly` | 温かく親切な口調 |
| `business` | ビジネスに適した簡潔で礼儀正しい表現 |
| `formal` | 丁寧語・敬語を用いたフォーマルな文体 |
| `netslang` | ネットスラングや掲示板風の文体 |
| `tweet` | SNS（X / Twitter）のつぶやきのような短い表現 |
| `literal` | 複数の訳し方がある場合に直訳に近い表現を選択 |

#### 2. サーバー用語集の登録 (`/add-glossary`)
固有名詞、ゲーム内の用語、キャラクター名、サーバー独自のスラングなどを登録することで、意図しない誤訳を防ぐことができます（最大 50 件）。
- **属性（`attribute`）**: 「人名」「地名」「スラング」「略語」「専門用語」などの属性を付与することで、AI が文脈をより正確に把握します。
- **常時適用（`always_include`）**: `true` に設定すると、メッセージ内にその単語が含まれていない場合でも常にコンテキストとして LLM に渡されます。

#### 3. フォーラム／メディアチャンネルのタグ連携 (`/edit-forum-tags`)
フォーラムチャンネル同士をグループ化する場合、言語ごとに異なるタグ ID を対応付けることができます。ある言語のフォーラムでタグ付きの投稿が行われると、ミラー先のフォーラムでも対応するタグが自動付与されます。

#### 4. 自動送信元の許可 (`/bot-whitelist`)
通常、Bot や Webhook による発言は無限ループ防止のため自動翻訳の対象外となりますが、`/bot-whitelist add` を使用して特定の Bot や Webhook を明示的に許可することで、アナウンス用 Bot や RSS 通知なども翻訳・ミラーリングすることが可能です。

---

## 2. エンジニア & セルフホスト向けガイド

### システム要件 & 技術スタック

- **言語**: Go 1.24 以上
- **データベース**: SQLite（`modernc.org/sqlite` による CGO 不要の純 Go 実装）
- **Discord ライブラリ**: `github.com/bwmarrin/discordgo`
- **翻訳エンジン**: OpenAI 互換 Chat Completions API（OpenAI, OpenRouter, Azure OpenAI, ローカル LLM 等）
- **クロスコンパイル**: CGO 不要（`CGO_ENABLED=0`）で Linux / Windows / macOS 向けにシングルバイナリを容易にビルド可能

---

### セルフホスト & 起動手順

#### 1. Discord Bot の作成
1. [Discord Developer Portal](https://discord.com/developers/applications) で新しい Application を作成します。
2. **Bot** タブで `MESSAGE CONTENT INTENT` を有効化し、Bot Token を取得します。
3. **OAuth2 → URL Generator** で `bot`, `applications.commands` スコープと必要な権限を選択し、招待リンクを作成してサーバーに追加します。

#### 2. OpenAI 互換 API の準備
利用する LLM プロバイダ（OpenAI, OpenRouter 等）のエンドポイント URL、API キー、モデル ID を用意します。

#### 3. 環境変数の設定
`.env.example` をコピーして `.env` を作成し、必要なパラメータを設定します。

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

#### 4. ビルドと実行

ローカルで直接実行する場合:
```sh
go run ./cmd/discord-auto-translator
```

バイナリをビルドして実行する場合:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**モデル接続検証オプション (`--model-prewarm`)**:
デプロイ時などに LLM プロバイダへの接続・認証およびレスポンス契約を事前に検証して終了するモードです（Discord や HTTP サーバーは起動しません）。
```sh
./discord-auto-translator --model-prewarm
```

---

### 環境変数リファレンス

| 変数名 | 必須 | デフォルト値 | 説明 |
|---|---|---|---|
| `DISCORD_TOKEN` | **必須** | - | Discord ボットのトークン |
| `OPENAI_BASE_URL` | **必須** | - | OpenAI 互換 Chat Completions のベース URL（例: `https://api.openai.com/v1`） |
| `OPENAI_API_KEY` | **必須** | - | API Bearer トークン |
| `OPENAI_MODEL` | **必須** | - | プロバイダ側で指定するモデル ID |
| `OPENAI_REASONING_EFFORT` | 任意 | (未設定) | Chat Completions の `reasoning_effort`。ハイブリッド推論モデルで思考トークンを抑制してレイテンシを下げたい場合は `none` を指定 |
| `DB_PATH` | 任意 | `./translator.db` | SQLite データベースファイルの保存パス |
| `HTTP_ADDR` | 任意 | `:8080` | アバターバッジサーバー用のアドレス |
| `PUBLIC_BASE_URL` | 任意 | (未設定) | アバターリングバッジ用の公開ベース URL。設定するとアバター画像の周囲に最上位ロール色ボーダーを付与します（未設定時は元画像を直接利用） |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 任意 | `100000` | サーバー（ギルド）ごとの 1 分間あたりの翻訳トークン上限 |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 任意 | `120` | アバターエンドポイントに対する IP 単位の 1 分間あたりのリクエスト上限 |
| `MESSAGE_LINK_RETENTION_DAYS` | 任意 | `0` | メッセージリンクデータの保持日数。`0` は無期限保持。数値を指定すると期限切れデータを 24 時間ごとに自動パージ |
| `GUILD_DATA_RETENTION_DAYS` | 任意 | `0` | Bot が退出したギルドのデータ保持日数。期限内に再参加すると削除予定はキャンセルされます |

---

### アーキテクチャ & 設計思想

#### 1. 翻訳パイプライン
1. **文脈収集 (Context Assembly)**: チャンネルのトピック、直近の会話履歴バースト、リプライ先メッセージ、共有 URL の OGP メタデータ、添付画像を収集します。
2. **プレースホルダー保護 (Placeholder Masking)**: メンション（`<@id>`）、絵文字（`<:name:id>`）、チャンネルリンク（`<#id>`）、URL、インラインコード・コードブロックなどを `[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]` などの形式に一時置換し、誤翻訳やプロンプトインジェクションを防止します。
3. **プロンプト合成 & キャッシュ最適化**: システムプロンプト、安定コンテキスト、履歴コンテキスト、可変コンテンツに整理し、LLM 側のプレフィックスプロンプトキャッシュが効きやすい構造で送信します。
4. **Structured Outputs による一括生成**: `response_format.type=json_schema`（`strict: true`）を用いて、全対象言語への翻訳結果を 1 回の API 呼び出しで構造化 JSON として取得します。
5. **後処理 & ミラーリング**: プレースホルダーを復元し、グループ内の Discord リンク書き換えや `hreflang` 代替 URL 置換を適用した上で、Webhook 経由で対象チャンネルへ並行配信します。

#### 2. 安全性 & Fail-Closed 設計
- **プロンプトインジェクション対策**: すべてのユーザー入力は XML エスケープされ、信頼できない入力として隔離されます。プレースホルダー化によりシステム指示の上書きを防止します。
- **Fail-Closed 原則**: トークン数超過（`finish_reason=length`）、不正な JSON、一時的な通信エラー等が発生した場合は、誤った内容や中途半端なメッセージをミラーリングせず、送信元チャンネルへローカライズされたエラー通知を返します。

#### 3. 信頼性とデータ整合性
- **冪等性 (Idempotency)**: イベントの重複受信や並行処理に対応するため、`message_links` と `processed_events` による二重配信防止機構を備えています。
- **補償トランザクション**: Webhook 投稿成功後に DB 保存が失敗した場合、投稿した Discord メッセージを即座に削除して孤児メッセージの発生を防ぎます。
- **双方向同期**: ピン留めやリアクションは、送信元・ミラー先のどちらで操作されても、全ペアメッセージ間で状態が自動同期されます。

---

### 開発 & テスト

#### テストの実行
```sh
go test ./...
```

#### 多言語 UI カタログ (i18n)
ユーザー向けのエラーメッセージや通知文言はすべて `internal/translatorbot/ui_strings.go` で一元管理されており、13 言語に対応しています。新しい文言を追加する場合はカタログの全言語分を定義し、網羅性テスト（`TestUIStringCatalogIsComplete`）でフォーマットの整合性を検証します。

---

## 3. ライセンス

本プロジェクトは [MIT License](LICENSE) のもとで公開されています。
