# 開発者向け注意事項・補足情報

## 1. アーキテクチャの全体像

```
cmd/discord-auto-translator/
└── main.go                 # エントリポイント。Discord イベントを受け取り Service に渡す

internal/translatorbot/
├── config.go               # 環境変数・.env の読み込み
├── models.go               # データ構造体の定義
├── store.go                # SQLite CRUD（唯一の永続化レイヤー）+ sentinel エラー定義
├── translator.go           # provider-neutral なプロンプト構築・応答パース
├── bedrock_translator.go   # Amazon Bedrock Mantle Responses API の翻訳クライアント
├── debug_log.go            # 翻訳往復のデバッグログ（JSON Lines・任意有効化）
├── placeholders.go         # 翻訳前後のプレースホルダー保護・復元
├── service.go              # Service 本体・翻訳フロー共通処理（translateWithLimit）・通知
├── service_message.go      # 通常メッセージのミラー・編集・削除・リプライ引用
├── service_forward.go      # 転送メッセージ（FORWARD）のミラー
├── service_thread.go       # スレッド作成・更新・削除・スレッド内メッセージ同期
├── service_sync.go         # リアクション・ピン留め同期
├── content.go              # 本文加工の純粋関数（添付URL化・疑似リプライ解析・切り詰め等）
├── ui_strings.go           # 全ユーザー向け文言の多言語カタログ（13言語 + 英語フォールバック）
├── commands.go             # スラッシュコマンドの定義・ロケール対応ハンドラ
├── styles.go               # 翻訳スタイルプリセット定義・検証
├── discord_client.go       # DiscordAPI インターフェース + discordgo 実装
├── discord_links.go        # 翻訳後テキスト内の Discord リンク・メンション置換
├── discord_retry.go        # Discord API のレート制限リトライ
├── ratelimit.go            # ギルド単位の翻訳トークンレートリミッター
├── languages.go            # 言語コード検証・オートコンプリート候補
├── avatar.go               # アバター画像バッジ（オレンジリング）
└── url_page.go             # URL 単位ページキャッシュ（OGP メタ + hreflang 置換）
```

### ユーザー向け文言の多言語化 (i18n)

ユーザーに表示されるすべての文言（コマンド応答・エラー・通知・疑似リプライのラベル等）は `ui_strings.go` の `uiStrings` カタログで管理されます。

- 文言は `uiKey` 定数で識別し、`localizedUIString` / `localizedUIStringf` で取得します。ハードコードされた日本語・英語文字列をコードに直接書かないでください。
- **スラッシュコマンド応答**は Discord の `interaction.Locale` を `resolveUILanguage` で解決した言語で返します。
- **チャンネルへの通知**（レート制限・翻訳失敗）と**疑似リプライ・転送見出し**は、対象チャンネルに登録された言語を使用します。
- 未対応言語は英語にフォールバックします。`zh-CN` / `zh-TW` / `pt-BR` は地域付きで解決し、その他は基本言語（`de-AT` → `de`）に縮約します。
- キーの追加時は**全言語**にエントリを追加してください。`TestUIStringCatalogIsComplete` がキーの網羅性とフォーマット動詞（`%[1]s` 等）の一致を検証します。
- ストア層・検証層は文言を持たず sentinel エラー（`ErrGroupNotFound`, `ErrGlossaryFull`, `ErrStyleCustomTooLong` 等）を返し、`commands.go` の `replyGroupError` などがカタログの文言へマップします。予期しないエラーはログに記録し、ユーザーには汎用メッセージ（`uiKeyUnexpectedError`）のみを表示します。

---

## 2. 依存関係の注意点

### discordgo バージョン固定

`discordgo v0.29.0` を使用しています。このバージョンは Discord の一部新しい API に未対応の場合があります。

**スレッドの webhook 操作** (`EditWebhook` / `DeleteWebhook` でのスレッド内メッセージ操作) は discordgo の公式メソッドが `thread_id` に対応していないため、`discord_client.go` 内で `session.RequestWithBucketID` を直接呼び出す実装になっています（`webhookMessageURL` 関数）。discordgo をアップデートする場合はこの部分の互換性を確認してください。

### SQLite: CGO 不要

`modernc.org/sqlite` を使用しており CGO は不要です。`CGO_ENABLED=0` でクロスコンパイルできます。デプロイスクリプトもこれを前提にしています。

### Amazon Bedrock と Gemma 4 26B-A4B

`bedrock_translator.go` でモデルとリクエストパラメータが定義されています。リージョンとProject IDは `config.go` が環境変数から必須設定として読み込みます：

```go
const bedrockModel = "google.gemma-4-26b-a4b"
// runtime timeout: 30s
// temperature: omitted (Gemma provider default 1.0), max_output_tokens: 4096
```

Gemma 4は `bedrock-runtime`、Invoke、Converseに対応しないため、`AWS_BEDROCK_REGION` から組み立てた非ストリーミングMantle Responses APIへHTTPリクエストを送り、`OpenAI-Project` に `AWS_BEDROCK_PROJECT_ID` を設定してから、AWS SDK for Go v2のSigV4 signerでサービス名 `bedrock-mantle` として署名します。静的 credentials provider に `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` を明示し、SDK credential chainへフォールバックしません。HTTP再試行は実装しません。`store=false` 固定です。Mantleはrequest metadata非対応なのでDiscord IDは送らず、既定ではプロンプト・応答・認証情報・AWSエラーメッセージをログへ出しません（下記のデバッグログを有効化した場合を除く）。HTTP失敗時は許可文字を制限したtype、code、param、request IDだけを診断情報として返します。

Gemma 4 26B-A4BはBedrock Structured Outputs非対応です。固定JSON Schemaはsystem instructionへ含め、レスポンスはcompleted状態、単一のassistant `output_text`、usage、JSON件数・順序・言語タグ・空文字・未知フィールドをすべて検証してfail-closedにします。Responses APIが返すreasoning itemは最終textとして扱いません。

全対象言語を1リクエストで生成します。全リクエストで同一のBedrock対応schemaを使い、件数・順序・言語タグ・空文字は既存パーサーで厳密検証します。4K出力上限への到達、不正JSON、異常stop reasonはfail-closedです。分割、retry、別providerへのfallbackはありません。

`--bedrock-prewarm` はDiscord・SQLite・HTTPサーバーを起動せず、認証情報・モデルアクセス・レスポンス契約を最大5分で検証して終了します。デプロイスクリプトはprewarm成功後だけ稼働バイナリを置換します。

### 翻訳デバッグログ（`debug_log.go`）

翻訳失敗の原因調査用に、`TRANSLATION_DEBUG_LOG_PATH` を設定したときだけ `translatePrepared` の1往復を1行のJSONとして追記します。パーサーが捨てる情報（reasoning itemのテキスト、`usage` の内訳、未知フィールド、非2xx時のAWSエラー本文）を欠落なく残すため、構造体ではなく**送信したpayloadバイト列と受信本文バイト列そのもの**を記録します。

```json
{"time":"...","guild_id":"...","message_id":"...","target_languages":["en"],"duration_ms":812,"request":{...},"http_status":200,"response":{...},"error":"..."}
```

- `error` は encode・署名・transport（タイムアウト含む）・HTTP・レスポンス契約・JSONパースの全経路を `translatePrepared` の `defer` で拾います。レスポンスがJSONとして不正な場合だけ `response` の代わりに `response_text` へ生文字列を入れます。
- `guild_id` / `message_id` はDiscord側と突き合わせるためのローカル記録で、Mantleへは従来どおり送りません。
- `main.go` が起動時にファイルを開き、開けなければ `log.Fatal` で停止します（`--bedrock-prewarm` でも有効）。書き込み失敗は翻訳を止めず stderr へ出します。
- 本文全量を書くためディスクを圧迫します。`0600` で作成し、64 MiB を超えると `<path>.1` へ1世代だけローテートします。**プライバシーポリシーのメッセージ関連データ60日以内削除に合わせ、調査が終わったらログを削除してください。**

確認用 CLI（直近50件の要約。`.1` ローテートも自動読込。パス未指定時は `TRANSLATION_DEBUG_LOG_PATH` → `.env` → `./translation-debug.log`）:

```sh
go run ./cmd/inspect-translation-log
go run ./cmd/inspect-translation-log --errors --detail
go run ./cmd/inspect-translation-log --message-id <id> --detail
```

GCE 上のログを取る場合は、ローカル `.env` に `TRANSLATION_DEBUG_LOG_PATH` を書き、`-UploadEnv` で同期する（inspect 側では設定をいじらない）:

```powershell
.\deploy\deploy-gce.ps1 -UploadEnv
# 翻訳を1回発生させてから:
.\deploy\inspect-translation-log.ps1 -Remote -Errors -Detail
```

`jq` で失敗だけを絞り込む例:

```sh
jq -c 'select(.error) | {time, guild_id, message_id, http_status, duration_ms, error}' translation-debug.log
```

---

## 3. 未実装・未接続の機能

### メッセージ同期の信頼性（形式検証後に実装済み）

- **冪等性**: 各ターゲット送信前に `message_links` と `processed_events`（キー: `msglink:{sourceChannel}:{sourceMessage}:{targetChannel}`）を確認し、既に同期済みならスキップします。同一 `(channelID, messageID)` の並行処理は `messageLocks` で直列化します。
- **補償トランザクション**: `SendWebhook` 成功後に `SaveMessageLink` が失敗した場合、`DeleteWebhook` で Discord 上の投稿を削除します（`sendAndSaveLink`）。
- **best-effort fan-out**: 複数ターゲットへの転送中に一部が失敗しても残りは続行し、エラーは `errors.Join` で集約して返します。
- **ピン留め同期**: `MESSAGE_UPDATE` で `pin_states` テーブルに保存済みの状態と `Pinned` を比較し、変化時のみ `SyncPin` を実行します。Webhook ミラー側のピン留めも双方向に同期し、bot 自身のピン操作によるエコーは状態比較で抑止します。
- **内容不変の編集スキップ**: ピン留めなど本文が変わらない `MESSAGE_UPDATE` では `source_content_snapshot` と比較して再翻訳をスキップします。
- **転送snapshotの再利用**: `FORWARD` は immutable な `message_snapshots[0]` から取り込みます。送信先に対応する既存ミラーがあれば翻訳済み本文を再利用し、対応がない外部本文だけを翻訳します。添付・ステッカーは既存のURL化処理を使い、保存snapshotには転送本文を記録します。

---

## 4. スラッシュコマンドの登録タイミング

```go
// main.go
translatorbot.RegisterGuildCommands(dg, dg.State.User.ID)
// GUILD_CREATE でも RegisterGuildCommandsForGuild を呼び出す
```

コマンドは **起動時に bot が参加しているすべてのギルドへ再登録** されます。新しいギルドに参加した場合も `GUILD_CREATE` イベントで同じコマンドを登録します。Discord のコマンド登録は `PUT` / `POST` でも既存コマンドを上書き可能なため、通常は問題ありませんが、以下の点に注意してください：

- **登録解除の仕組みがない**: コマンドを削除したい場合は Discord の Developer Portal から手動で削除するか、削除用のコードを一時的に追加する必要があります。
- **グローバルコマンドは未使用**: すべてギルドコマンドとして登録されます。
- **レート制限**: ギルド数が多い場合、起動時のコマンド登録でレート制限にかかる可能性があります。

---

## 5. スレッド同期の複雑性

スレッド同期は最も複雑なロジックです。`docs/discord-thread-message-sync.md` に詳細なパターンマトリクスがありますが、開発時の重要ポイントをまとめます。

### 遅延同期 (Defer) パターン

メッセージに紐づくスレッド（テキストチャンネルのメッセージからスレッドを作成した場合）は、ターゲット側の親メッセージ翻訳が完了していないと作成できません。

```
ソース: メッセージA → スレッドB を作成
            ↓
ターゲット: メッセージAの翻訳が存在する場合 → CreateThreadFromMessage
          メッセージAの翻訳がない場合 → DEFER（THREAD_STARTER_MESSAGE イベントまで待機）
```

`DeferWithoutSourceMsg = true` のとき、`createTargetThread` は空文字列を返してスキップし、後続の `THREAD_STARTER_MESSAGE` イベントで `ensureThreadSynced` が再試行します。

### ミューテックスによる直列化

```go
type Service struct {
    threadMu sync.Mutex
    messageLocks sync.Map // (channelID, messageID) 単位の直列化
}
```

`syncThreadCreate` は `threadMu` でシリアライズされています。`HandleMessageCreate` は `messageLocks` で同一メッセージの並行処理を防ぎます。

### ウェブフックのスレッド内メッセージ操作

スレッド内のメッセージを操作する場合、ウェブフックの認証情報は**親チャンネルのもの**を使い、`thread_id` クエリパラメータを追加します：

```
PATCH /webhooks/{webhook.id}/{webhook.token}/messages/{message.id}?thread_id={thread.id}
```

`discord_client.go` の `threadIDForWebhook` と `webhookMessageURL` がこの処理を担います。

---

## 6. 翻訳プロンプトの設計

翻訳プロンプトは XML 構造になっています：

```xml
<translation_request>
  <target_languages>en, ja</target_languages>
  <discord_context>
    <server_name>My Server</server_name>
    <server_overview>...</server_overview>
    <channel_name>general</channel_name>
    <channel_topic>...</channel_topic>
  </discord_context>
  <recent_context>
    <message author="Alice">Hello!</message>
  </recent_context>
  <reply_context>
    <message author="Bob">Earlier reply target</message>
  </reply_context>
  <site_context>
    <site title="Example Article">Page description from OGP</site>
  </site_context>
  <final_message author="Carol">How are you? [SITE:Example Article]</final_message>
</translation_request>
```

- **すべてのユーザーコンテンツは XML エスケープされています。** `<`, `>`, `&` 等が含まれていても安全です。
- `<recent_context>` は翻訳グループ内の全会話ロケーション（親チャンネルまたは同期済みスレッド）から最大3件の原文を収集します。
- `<reply_context>` はリプライ先を最大3件遡った引用チェイン（古い順）です。`<recent_context>` より優先して解釈に使います。引用チェインに含まれるメッセージは `<recent_context>` から除外されます。
- `<site_context>` は本文中の共有 URL から取得した title / description です。`<site title>` は `[SITE:...]` プレースホルダのラベルと一致します。
- 履歴・リプライの `<message>` は `author`（表示名）と原文のみ。`lang` 属性は付けません。
- `<final_message>` はメッセージ翻訳時に `author` 属性へ投稿者表示名を付与します（スレッド名など author が無い場合は省略）。
- システムインストラクションはコンテンツを「信頼できない」として扱うよう明示的に指示しています。
- Gemma 4 26B-A4B のtemperatureはリクエストから省略し、モデル推奨かつBedrock既定の `1.0` を使用します。`max_output_tokens` はアプリケーション上限として `4096` 固定です。

---

## 7. テストの構造

`go test ./...` で全テストを実行できます。

### テストの設計方針

- `Store` のテストはインメモリ SQLite（`":memory:"`）を使用
- `Service` のテストは `fakeDiscordAPI` と `echoTranslator`（入力をそのまま返す）で Discord API と翻訳エンジンを差し替え
- `Translator` のテストはプロンプト構造と XML エスケープの正確性を検証

### モックの場所

| テスト対象 | モック実装 |
|---|---|
| Discord API | `service_test.go` の `fakeDiscordAPI` |
| 翻訳エンジン | `service_test.go` の `echoTranslator` |
| コマンド応答 | `commands_test.go` の `captureResponses`（`CommandHandler.respond` を差し替え） |
| HTTP クライアント | `url_page_test.go` / `avatar_test.go` のインライン `httptest` |

### テストで確認されていないこと

- 実際の Amazon Bedrock / Gemma 4 26B-A4B レスポンス
- 実際の Discord API との通信

---

## 8. ウェブフック名のサニタイズ

Discord の規約により、ウェブフック名に "discord" を含めることが禁止されています。`sanitizeWebhookName` がこれを処理します：

- `"discord"` → `"D-scord"` (大文字小文字問わず)
- 名前が 80 文字を超える場合は切り詰め
- 空白になった場合は `"Discord Auto Translator"` にフォールバック

ユーザー名にニックネームや表示名が使われるため、`discord` を含むユーザー名は自動的に変換されます。

---

## 9. 設定 (`config.go`) の詳細

| 環境変数 | 必須 | デフォルト | 説明 |
|---|---|---|---|
| `DISCORD_TOKEN` | 必須 | — | ボットのトークン |
| `AWS_ACCESS_KEY_ID` | 必須 | — | Bedrock専用 IAM ユーザーのアクセスキー ID |
| `AWS_SECRET_ACCESS_KEY` | 必須 | — | Bedrock専用 IAM ユーザーのシークレットアクセスキー |
| `AWS_BEDROCK_REGION` | 必須 | — | Bedrock Mantleリージョン |
| `AWS_BEDROCK_PROJECT_ID` | 必須 | — | Bedrock Mantle Project ID |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 任意 | `100000` | ギルドごとの Gemma 4 26B-A4B トークン上限/分 |
| `TRANSLATION_DEBUG_LOG_PATH` | 任意 | `""` | 翻訳往復のデバッグログの出力先。未設定でログを生成しない |
| `DB_PATH` | 任意 | `./translator.db` | SQLite ファイルのパス |
| `HTTP_ADDR` | 任意 | `:8080` | アバターバッジ HTTP サーバーのアドレス |
| `PUBLIC_BASE_URL` | 任意 | `""` | アバターバッジ URL のベース（末尾スラッシュなし） |

`.env` ファイルの読み込みは **すでに設定されている環境変数を上書きしません**。これにより systemd の `EnvironmentFile` と `.env` が共存できます。

---

## 10. DB スキーマ

`store.Init` は現在の完成形スキーマだけを作成・検証します。日時列は Unix milliseconds の `INTEGER`、パージ対象の Discord snowflake は `INTEGER` です。

**将来のマイグレーションも同様のパターンで追記してください。** バージョン管理されたマイグレーションツールは使用していません。

---

## 11. Gateway インテント

```go
dg.Identify.Intents = discordgo.IntentsGuilds |
    discordgo.IntentsGuildMessages |
    discordgo.IntentsGuildMessageReactions |
    discordgo.IntentsMessageContent
```

- `IntentsMessageContent` は **特権インテント** です。Discord Developer Portal でボットの設定ページから有効化が必要です。
- `IntentsGuildMembers` は使用していないため、メンバーのニックネームは `MessageCreate` の `Member` フィールドからのみ取得されます（キャッシュには依存しません）。

---

## 12. グローバル状態と並行性

- `Store` はシングルトン。`sql.DB` は内部でコネクションプールを持ちゴルーチンセーフです。
- `Service.threadMu` はスレッド作成処理のみをシリアライズします。
- `Service.messageLocks` は同一 `(channelID, messageID)` のメッセージ処理を直列化します。
- `Service.httpClient` は `http.DefaultClient` を共有します。
- それ以外に共有状態はなく、各イベントハンドラは独立して実行されます。

---

## 13. エラーハンドリングの方針

- すべてのエラーは `main.go` の各ハンドラで `log.Printf` してから処理を継続します（ボットは落ちません）。翻訳失敗の詳細は `TRANSLATION_DEBUG_LOG_PATH` のデバッグログにのみ残ります。
- `service.go` 内では最初のエラーで即時 `return` します。途中で失敗した場合、成功したチャンネルへの投稿はロールバックされません。
- Discord API エラー（レート制限・ネットワーク障害など）はリトライしません。

---

## 14. 既知の制約・注意事項

### メッセージ内容がない場合

`HandleMessageCreate` は本文が空でも、添付ファイルまたはステッカーがあればミラーリングします。アセットは再アップロードせず、署名クエリを除いた Discord CDN URL を本文末尾へ追加します。

### ウェブフック由来メッセージの無視

ボット自身がウェブフックで投稿したメッセージに対してイベントが発火しても、`WebhookID != ""` のチェックでスキップされます。無限ループにはなりません。

### グループ解散時のウェブフック削除なし

`/leave-channel` や `/delete-group` を実行しても、Discord 側に作成されたウェブフックは削除されません。不要なウェブフックが残り続けます（Discord の制限: 1チャンネルあたり最大15個）。

### 同一チャンネルを複数グループに登録可能

1つのチャンネルが複数の翻訳グループに参加できます。その場合、メッセージはすべてのグループのチャンネルへ翻訳・投稿されます。

### スレッドアーカイブ

Discord がスレッドをアーカイブした場合の挙動は考慮されていません。アーカイブ済みスレッドへのウェブフック送信は Discord API エラーになります。

### `translationHistoryLimit` / `translationReplyChainLimit` / `translationHistoryMaxAge`

```go
const translationHistoryLimit = 3
const translationReplyChainLimit = 3
const translationHistoryMaxAge = 24 * time.Hour
```

翻訳文脈として使用する直近メッセージ数、引用チェインの最大遡り件数、履歴の時間窓はハードコードされています。引用チェインには時間窓を適用しません。
