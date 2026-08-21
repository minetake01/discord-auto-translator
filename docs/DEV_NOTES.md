# 開発者向け注意事項・補足情報

## 1. アーキテクチャの全体像

```
cmd/discord-auto-translator/
└── main.go                 # エントリポイント。Discord イベントを受け取り Service に渡す

internal/translatorbot/
├── config.go               # 環境変数・.env の読み込み
├── models.go               # データ構造体の定義
├── store.go                # SQLite Open/Init/スキーマ + グループ/リンク/スレッド等 CRUD + sentinel エラー
├── store_guild.go          # ギルドライフサイクル・保持期限パージ
├── store_glossary.go       # 用語集 CRUD
├── store_topic.go          # 世代切替で捨てた会話の話題要約
├── translator.go           # provider-neutral なプロンプト構築・応答パース
├── openai_translator.go    # OpenAI 互換 Chat Completions の翻訳クライアント
├── debug_log.go            # 翻訳往復のデバッグログ（JSON Lines・任意有効化）
├── placeholders.go         # 翻訳前後のプレースホルダー保護・復元
├── service.go              # Service 本体・翻訳フロー共通処理（translateWithLimit）・通知
├── service_message.go      # 通常メッセージのミラー・編集・削除・リプライ引用
├── service_forward.go      # 転送メッセージ（FORWARD）のミラー
├── service_thread.go       # スレッド作成・更新・削除・スレッド内メッセージ同期
├── service_sync.go         # リアクション・ピン留め同期
├── content.go              # 本文加工の純粋関数（添付URL化・疑似リプライ解析・切り詰め等）
├── image.go                # 画像添付判定・ダウンロード・ビジョン用縮小・再アップロード用バイト保持
├── ui_strings.go           # 全ユーザー向け文言の多言語カタログ（13言語 + 英語フォールバック）
├── commands.go             # スラッシュコマンド定義・ギルド登録
├── command_handlers.go     # CommandHandler と各コマンド処理
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
- **チャンネルへの通知**（レート制限・翻訳失敗）は投稿元メッセージへのリプライとして送り、**疑似リプライ・転送見出し**とともに対象チャンネルに登録された言語を使用します。プロバイダ一時障害（タイムアウト・通信エラー・HTTP 429/5xx）と自前のトークンレート制限は投稿元チャンネルごとに状態継続中は最初の1回だけ通知し、制限では制限解除まで、プロバイダ障害では外部の翻訳サービス復旧まで、しばらく翻訳されない可能性がある旨を付けます。プロバイダ障害は翻訳 API 成功で、レート制限は次に翻訳が許可された時点で解除します。
- 未対応言語は英語にフォールバックします。`zh-CN` / `zh-TW` / `pt-BR` は地域付きで解決し、その他は基本言語（`de-AT` → `de`）に縮約します。
- キーの追加時は**全言語**にエントリを追加してください。`TestUIStringCatalogIsComplete` がキーの網羅性とフォーマット動詞（`%[1]s` 等）の一致を検証します。
- ストア層・検証層は文言を持たず sentinel エラー（`ErrGroupNotFound`, `ErrGlossaryFull`, `ErrStyleCustomTooLong` 等）を返し、`commands.go` の `replyGroupError` などがカタログの文言へマップします。予期しないエラーはログに記録し、ユーザーには汎用メッセージ（`uiKeyUnexpectedError`）のみを表示します。

---

## 2. 依存関係の注意点

### discordgo バージョン固定

`discordgo v0.29.0` を使用しています。このバージョンは Discord の一部新しい API に未対応の場合があります。

**スレッドの webhook 操作** (`EditWebhook` / `DeleteWebhook` でのスレッド内メッセージ操作) は discordgo の公式メソッドが `thread_id` に対応していないため、`discord_client.go` 内で `session.RequestWithBucketID` を直接呼び出す実装になっています（`webhookMessageURL` 関数）。discordgo をアップデートする場合はこの部分の互換性を確認してください。

**添付の代替テキスト** discordgo v0.29.0 の `MessageAttachment` に `description` が無いため、Gateway の `Event.RawData` から読み、webhook / forum 初回投稿では `payload_json.attachments[].description` を自前の multipart で送ります。

### SQLite: CGO 不要

`modernc.org/sqlite` を使用しており CGO は不要です。`CGO_ENABLED=0` でクロスコンパイルできます。

### OpenAI 互換 Chat Completions

`openai_translator.go` でリクエストパラメータが定義されています。ベース URL・API キー・モデルは `config.go` が環境変数から必須設定として読み込みます：

```go
// per-attempt timeout: 60s
// transient retry: exactly once after 1s (timeout / transport / HTTP 429+5xx)
// temperature: omitted (provider default), max_tokens: 4096
// reasoning_effort: omitted unless OPENAI_REASONING_EFFORT is set
// endpoint: POST {OPENAI_BASE_URL}/chat/completions
```

`OPENAI_BASE_URL`（例: `https://api.openai.com/v1`）に対して非ストリーミング Chat Completions へ HTTP リクエストを送り、`Authorization: Bearer {OPENAI_API_KEY}` で認証します。モデルは `OPENAI_MODEL` をそのまま送ります。`OPENAI_REASONING_EFFORT` を設定したときだけ Chat Completions の `reasoning_effort` を送ります（未設定は省略）。ハイブリッド推論モデルでは `none` で思考トークンを省略できます。試行ごとに60秒の期限を付け、タイムアウト・通信エラー・HTTP 429/5xx に限り1秒待機してちょうど1回だけ再試行します。契約違反や4xx（429以外）は再試行しません。Discord ID は送らず、既定ではプロンプト・応答・認証情報・プロバイダーエラー本文をログへ出しません（下記のデバッグログを有効化した場合を除く）。HTTP失敗時は許可文字を制限したtype、code、param、request IDだけを診断情報として返します。

Structured Outputs は `response_format.type=json_schema`（`strict: true`）で指定し、用途別の固定JSON Schemaで制約付きデコードします。`language` は当該リクエストの target languages の BCP-47 enum です。system instructionへschemaは複製しません。レスポンスは単一 choice、非空の assistant content、usage、JSON件数・順序・言語タグ・空文字・未知フィールドをすべて検証してfail-closedにします。`finish_reason=length` は truncate として拒否します。OpenRouter（`openrouter.ai`）では `provider.require_parameters=true` を付け、Structured Outputs非対応エンドポイントへは送りません。

全対象言語を1リクエストで生成します。schema の構造は用途ごとに固定し、`language` enum だけ対象言語に合わせます。件数・順序・言語タグ・空文字は既存パーサーで厳密検証します。4K出力上限への到達、不正JSON、異常finish reasonはfail-closedです。分割や別providerへのfallbackはありません。

`--model-prewarm` はDiscord・SQLite・HTTPサーバーを起動せず、認証情報・モデルアクセス・レスポンス契約を最大5分で検証して終了します。デプロイスクリプトはprewarm成功後だけ稼働バイナリを置換します。

### 翻訳デバッグログ（`debug_log.go`）

翻訳失敗の原因調査と、翻訳精度・キャッシュ効率・料金の効果測定用に、`TRANSLATION_DEBUG_LOG_PATH` を設定したときだけ `translatePrepared` の1往復を1行のJSONとして追記します（一時障害リトライ時は最大2行）。パーサーが捨てる情報（未知フィールド、非2xx時のエラー本文）を欠落なく残すため、**送信したpayloadバイト列と受信本文バイト列そのもの**に加え、測定しやすい一次フィールドを記録します。

```json
{"time":"...","ended":"...","duration_ms":812,"wait_ms":800,"read_ms":5,"attempt":1,"response_created":1700000000,"processing_ms":790,"guild_id":"...","message_id":"...","model":"...","schema_name":"message_translations","target_languages":["en"],"prompt_cache_key":"...","prompt_cache_ttl_sent":false,"prompt_cache_hit":true,"system_instruction":"...","user_prompt_frozen":"...","user_prompt_variable":"...","usage":{"prompt_tokens":1200,"cached_tokens":800,"completion_tokens":40,"cost_usd":0.00014},"request":{...},"http_status":200,"response":{...},"error":"..."}
```

- `system_instruction` / `user_prompt_frozen` / `user_prompt_variable` は実際に合成したプロンプト本文です（リクエストの image data URL は含めません）。
- `prompt_cache_hit` はプロバイダーが `usage.prompt_tokens_details.cached_tokens`（または同等フィールド）を報告し、その値が 1 以上のとき `true`、0 のとき `false`。報告が無い場合はフィールド自体を省略します。
- `prompt_cache_ttl_sent` はこの往復でキャッシュ TTL write を送ったかどうかです（ヒットそのものではありません）。
- `usage.cost_usd` はプロバイダーが `usage.cost` を返したときだけ記録します（OpenRouter は USD）。単価表からの推計はしません。
- 時間は同じ往復だけで測ります。`duration_ms` は試行全体、`wait_ms` は HTTP 応答ヘッダ待ち、`read_ms` は本文読み取り、`ended` は終了時刻、`attempt` は 1 始まりの試行番号です。追加の generation API は呼びません。
- 同じ応答に含まれるときだけ `response_created`（本文の `created`）、`processing_ms`（`openai-processing-ms` ヘッダ）、`server_timing`、`usage.total_time` / `queue_time` / `prompt_time` / `completion_time` を残します。
- `error` は encode・transport（タイムアウト含む）・HTTP・レスポンス契約・JSONパースの全経路を `translatePrepared` の `defer` で拾います。レスポンスがJSONとして不正な場合だけ `response` の代わりに `response_text` へ生文字列を入れます。
- `guild_id` / `message_id` はDiscord側と突き合わせるためのローカル記録で、プロバイダーへは送りません。
- `main.go` が起動時にファイルを開き、開けなければ `log.Fatal` で停止します（`--model-prewarm` でも有効）。書き込み失敗は翻訳を止めず stderr へ出します。
- 本文全量を書くためディスクを圧迫します。`0600` で作成し、64 MiB を超えると `<path>.1` へ1世代だけローテートします。**プライバシーポリシーのメッセージ関連データ60日以内削除に合わせ、調査が終わったらログを削除してください。**

確認用 CLI（直近50件の要約。`.1` ローテートも自動読込。パス未指定時は `TRANSLATION_DEBUG_LOG_PATH` → `.env` → `./translation-debug.log`）。要約行は cache / cost を含みます。`--detail` はソース抜粋・翻訳・キャッシュ・料金・所要時間・usage・エラーを、`--prompt` は合成プロンプト全文を、`--stats` はフィルタ後のキャッシュヒット率・料金合計・所要時間の min/avg/max を出します:

```sh
go run ./cmd/inspect-translation-log --stats
go run ./cmd/inspect-translation-log --errors --detail
go run ./cmd/inspect-translation-log --message-id <id> --detail --prompt
```

`jq` で失敗だけを絞り込む例:

```sh
jq -c 'select(.error) | {time, guild_id, message_id, http_status, duration_ms, error}' translation-debug.log
jq -c '{time, message_id, prompt_cache_hit, usage}' translation-debug.log
```

---

## 3. 未実装・未接続の機能

### メッセージ同期の信頼性（形式検証後に実装済み）

- **冪等性**: 各ターゲット送信前に `message_links` と `processed_events`（キー: `msglink:{sourceChannel}:{sourceMessage}:{targetChannel}`）を確認し、既に同期済みならスキップします。同一 `(channelID, messageID)` の並行処理は `messageLocks` で直列化します。
- **補償トランザクション**: `SendWebhook` 成功後に `SaveMessageLink` が失敗した場合、`DeleteWebhook` で Discord 上の投稿を削除します（`sendAndSaveLink`）。
- **best-effort fan-out**: 複数ターゲットへの転送中に一部が失敗しても残りは続行し、エラーは `errors.Join` で集約して返します。
- **ピン留め同期**: `MESSAGE_UPDATE` で `pin_states` テーブルに保存済みの状態と `Pinned` を比較し、変化時のみ `SyncPin` を実行します。Webhook ミラー側のピン留めも双方向に同期し、bot 自身のピン操作によるエコーは状態比較で抑止します。
- **内容不変の編集スキップ**: ピン留めなど本文が変わらない `MESSAGE_UPDATE` では `source_content_snapshot` と比較して再翻訳をスキップします。
- **転送snapshotの再利用**: `FORWARD` は immutable な `message_snapshots[0]` から取り込みます。送信先に対応する既存ミラーがあれば翻訳済み本文を再利用し、対応がない外部本文だけを翻訳します。画像添付は再アップロード、非画像・ステッカーは CDN URL 追記です。保存snapshotには転送本文を記録します。

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
  <topic_summary>Earlier they were coordinating a delayed shipment.</topic_summary>
  <recent_context>
    <message author="Alice">見て<image index="1" filename="photo.png"></image></message>
  </recent_context>
  <reply_context>
    <message author="Bob">Earlier reply target</message>
  </reply_context>
  <site_context>
    <site id="1" title="Example Article">Page description from OGP</site>
  </site_context>
  <attachments>
    <attachment index="1" filename="sign.png">既存の代替テキスト</attachment>
  </attachments>
  <final_message author="Carol">How are you? [SITE:1]</final_message>
</translation_request>
```

- **すべてのユーザーコンテンツは XML エスケープされています。** `<`, `>`, `&` 等が含まれていても安全です。
- `<recent_context>` は翻訳グループ内の全会話ロケーション（親チャンネルまたは同期済みスレッド）から、同一バーストの原文を束ね後枠として積み上げます。隣接間隔が 15 分を超えると古い側を切り、件数・時間幅・トークンのハイウォーターで世代を切り替えます。履歴 0 件のときはセクション自体を出しません。本文が空でも画像添付がある投稿は残し、含まれた画像は `<image index>` で示します。
- 世代切替で捨てた枠は翻訳を待たせず裏で短く要約し、次のメッセージから凍結ユーザーパート先頭の `<topic_summary>` に載せます。要約が未完了なら履歴だけで翻訳します。沈黙 15 分でバーストが切れた要約は使いません。
- ユーザープロンプトは凍結パート（`always_include` glossary、任意の `<topic_summary>`、`<recent_context>` の途中まで）と可変パート（末尾枠・閉じタグ・本文マッチ glossary・reply/site/attachments/final）に分かれます。凍結パートは世代内で追記だけします。要約が載った時点で `prompt_cache_key` を分けます。
- `<reply_context>` はリプライ先を最大 3 件遡った引用チェイン（古い順、時間制限なし）です。`<recent_context>` より優先して解釈に使います。凍結済み履歴枠はリプライ先と重複しても残し、可変末尾だけ同一投稿なら除外します。画像は履歴と同じ `<image>` で示し、同一投稿の画像は同じ index を共有します。
- `<site_context>` は本文中の共有 URL から取得した title / description です。`<site id>` は `[SITE:N]` プレースホルダの N と一致します。title は背景情報であり、プレースホルダには含めません。読み込めた `og:image` は `<site>` 内の `<image>` として示し、ビジョン入力の文脈画像として渡します。
- `<attachments>` は現在メッセージの画像添付です。ビジョン入力はテキスト（breakpoint の後）の後ろに置き、現在メッセージの添付、履歴/リプライの `<image>`、OGP の順です。既存 alt だけ翻訳し、alt が空なら空文字を返します。生成はしません。現在メッセージの画像も履歴・リプライ・OGP と同様、取得・縮小失敗時はスキップします。`attachment_descriptions` の余剰要素は背景画像向けとして無視し、再アップロードには使いません。
- 履歴・リプライの `<message>` は `author`（表示名）と原文、任意の `<image>`。`lang` 属性は付けません。
- `<final_message>` はメッセージ翻訳時に `author` 属性へ投稿者表示名を付与します（スレッド名など author が無い場合は省略）。
- システムインストラクションはコンテンツを「信頼できない」として扱うよう明示的に指示しています。history / topic_summary / reply / site / style / glossary の適用方法は常に入れ、用語の実データは system に置きません。`always_include` glossary は凍結ユーザーパート、本文マッチ glossary は可変ユーザーパートへ出します。
- メッセージ翻訳の JSON Schema は画像の有無で変えず、`attachment_descriptions` を常に required にします。添付なしは空配列、添付ありは `<attachment>` と同順で少なくとも同数です。余剰要素は適用しません。`language` はリクエストの target languages の BCP-47 enum です。投票の回答数や添付枚数などリクエスト固有の件数は schema にも system にも書きません。
- Chat Completions リクエストは `prompt_cache_key` を付け、凍結テキストパートに `prompt_cache_breakpoint` を置きます。そのキーを未保持のときだけ `prompt_cache_options.ttl=1h` を送り、期限内の再利用では ttl を付けません。
- temperatureはリクエストから省略し、プロバイダー既定値を使用します。`reasoning_effort` は `OPENAI_REASONING_EFFORT` 未設定時は省略します。`max_tokens` はアプリケーション上限として `4096` 固定です。

---

## 7. テストの構造

`go test ./...`（CI では `go test -race ./...`）で全テストを実行できます。

### テストの設計方針（要件ガードレール）

テストは実装の镜像ではなく、**要件のガードレール**として書く。

1. **観測可能な振る舞いだけを検証する** — webhook 送信内容、DB 永続化結果、HTTP レスポンス、エラー契約、CLI 出力など。
2. **実装詳細を検査しない** — 内部定数値の自己比較、プライベートフィールド、`EXPLAIN QUERY PLAN` / PRAGMA、ロック保持プロトコルの振付は禁止。
3. **文字列は性質だけ見る** — プロンプトや UI 文言の完全一致スナップショットは書かない。XML エスケープ済みであること、指定セクションが含まれること、疑似リプライ構造であることなど、SPEC が定める性質に限定する。
4. **並行性はレースディテクタで担保する** — ロック振付の単体テストではなく CI の `go test -race` を使う。

テストファイルは SPEC の要件ドメイン単位に分ける（`mirror_test.go`、`reply_test.go`、`threadsync_test.go` など）。各テストは SPEC 節をコメントで明示する。

### テスト基盤

| テスト対象 | モック実装 |
|---|---|
| Discord API | `harness_test.go` の `fakeDiscordAPI` |
| 翻訳エンジン | `harness_test.go` の `echoTranslator` |
| Store | `harness_test.go` の `newTestStore`（インメモリ SQLite） |
| コマンド応答 | `commands_test.go` の `captureResponses`（`CommandHandler.respond` を差し替え） |
| HTTP クライアント | `sitemeta_test.go` / `avatar_test.go` / `openai_test.go` のインライン `httptest` 等 |

### テストで確認されていないこと

- 実際の OpenAI 互換プロバイダーレスポンス
- 実際の Discord API との通信

---

## 8. ウェブフック名のサニタイズ

Discord の規約により、ウェブフック名に "discord" を含めることが禁止されています。`sanitizeWebhookName` がこれを処理します：

- `"discord"` → `"D-scord"` (大文字小文字問わず)
- 名前が 80 文字を超える場合は切り詰め
- 空白になった場合はデフォルト名をサニタイズした値（`"D-scord Auto Translator"`）にフォールバック

ユーザー名にニックネームや表示名が使われるため、`discord` を含むユーザー名は自動的に変換されます。

---

## 9. 設定 (`config.go`) の詳細

| 環境変数 | 必須 | デフォルト | 説明 |
|---|---|---|---|
| `DISCORD_TOKEN` | 必須 | — | ボットのトークン |
| `OPENAI_BASE_URL` | 必須 | — | OpenAI 互換 Chat Completions のベース URL（例: `https://api.openai.com/v1`） |
| `OPENAI_API_KEY` | 必須 | — | Bearer 認証用 API キー |
| `OPENAI_MODEL` | 必須 | — | プロバイダー側モデル ID |
| `OPENAI_REASONING_EFFORT` | 任意 | 省略 | Chat Completions の `reasoning_effort`。未設定ではフィールド自体を送らない。`none` / `minimal` / `low` / `medium` / `high` / `xhigh` / `max` |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 任意 | `100000` | ギルドごとの翻訳トークン上限/分 |
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
- 話題要約は翻訳完了を待たず別ゴルーチンで生成し、`topicSummaryAttempts` で世代ごとの二重実行を抑止します。
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

`HandleMessageCreate` は本文が空でも、添付ファイルまたはステッカーがあればミラーリングします。画像添付は再アップロードし、既存の代替テキストがある場合のみ翻訳して付与します。ダウンロードや縮小に失敗した画像は再アップロードせず、署名クエリを除いた Discord CDN URL を本文末尾へ追加します。非画像添付とステッカーは署名クエリを除いた Discord CDN URL を本文末尾へ追加します。

### ウェブフック由来メッセージの無視

ボット自身がウェブフックで投稿したメッセージに対してイベントが発火しても、`WebhookID != ""` のチェックでスキップされます。無限ループにはなりません。

### グループ解散時のウェブフック削除なし

`/leave-channel` や `/delete-group` を実行しても、Discord 側に作成されたウェブフックは削除されません。不要なウェブフックが残り続けます（Discord の制限: 1チャンネルあたり最大15個）。

### 同一チャンネルを複数グループに登録可能

1つのチャンネルが複数の翻訳グループに参加できます。その場合、メッセージはすべてのグループのチャンネルへ翻訳・投稿されます。

### スレッドアーカイブ

Discord がスレッドをアーカイブした場合の挙動は考慮されていません。アーカイブ済みスレッドへのウェブフック送信は Discord API エラーになります。

### 履歴バーストと `translationReplyChainLimit`

```go
const historyIdleGap = 15 * time.Minute
const historyCountHigh = 16
const historyCountLow = 8
const historySpanHigh = 30 * time.Minute
const historySpanLow = 15 * time.Minute
const historyTokenHigh = 800
const historyTokenLow = 400
const historyFetchLimit = 512
const translationReplyChainLimit = 3
```

翻訳文脈の直近履歴は同一バーストを append-only に積み、沈黙 15 分・件数 16/8・時間幅 30/15 分・トークン 800/400 で世代を切り替えます。切り捨て確定時に捨てた枠を裏で短く要約し、次の翻訳から `<topic_summary>` として凍結先頭へ載せます。引用チェインの最大遡り件数は 3 で、時間窓は適用しません。キャッシュ TTL は 1 時間です。
