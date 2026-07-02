# 開発者向け注意事項・補足情報

## 1. アーキテクチャの全体像

```
cmd/discord-auto-translator/
└── main.go                 # エントリポイント。Discord イベントを受け取り Service に渡す

internal/translatorbot/
├── config.go               # 環境変数・.env の読み込み
├── models.go               # データ構造体の定義
├── store.go                # SQLite CRUD（唯一の永続化レイヤー）
├── translator.go           # Gemini API 呼び出し・プロンプト構築
├── placeholders.go         # 翻訳前後のプレースホルダー保護・復元
├── service.go              # ビジネスロジック全体（最も重要なファイル）
├── commands.go             # スラッシュコマンドの定義・ハンドラ
├── discord_client.go       # DiscordAPI インターフェース + discordgo 実装
├── languages.go            # 言語コード検証・オートコンプリート候補
├── avatar.go               # アバター画像バッジ（オレンジリング）
└── url_alt.go              # hreflang 代替 URL への置換
```

---

## 2. 依存関係の注意点

### discordgo バージョン固定

`discordgo v0.29.0` を使用しています。このバージョンは Discord の一部新しい API に未対応の場合があります。

**スレッドの webhook 操作** (`EditWebhook` / `DeleteWebhook` でのスレッド内メッセージ操作) は discordgo の公式メソッドが `thread_id` に対応していないため、`discord_client.go` 内で `session.RequestWithBucketID` を直接呼び出す実装になっています（`webhookMessageURL` 関数）。discordgo をアップデートする場合はこの部分の互換性を確認してください。

### SQLite: CGO 不要

`modernc.org/sqlite` を使用しており CGO は不要です。`CGO_ENABLED=0` でクロスコンパイルできます。デプロイスクリプトもこれを前提にしています。

### Gemini モデル名

`translator.go` で定数として定義されています：

```go
const geminiModel = "gemini-3.1-flash-lite"
```

モデルを変えたい場合はここを変更してください。ただし応答形式（テキストのみ返す）は変わらないことを確認してください。

---

## 3. 未実装・未接続の機能

### メッセージ同期の信頼性（形式検証後に実装済み）

- **冪等性**: 各ターゲット送信前に `message_links` と `processed_events`（キー: `msglink:{sourceChannel}:{sourceMessage}:{targetChannel}`）を確認し、既に同期済みならスキップします。同一 `(channelID, messageID)` の並行処理は `messageLocks` で直列化します。
- **補償トランザクション**: `SendWebhook` 成功後に `SaveMessageLink` が失敗した場合、`DeleteWebhook` で Discord 上の投稿を削除します（`sendAndSaveLink`）。
- **best-effort fan-out**: 複数ターゲットへの転送中に一部が失敗しても残りは続行し、エラーは `errors.Join` で集約して返します。
- **ピン留め同期**: `MESSAGE_UPDATE` で `pin_states` テーブルに保存済みの状態と `Pinned` を比較し、変化時のみ `SyncPin` を実行します。Webhook ミラー側のピン留めも双方向に同期し、bot 自身のピン操作によるエコーは状態比較で抑止します。
- **内容不変の編集スキップ**: ピン留めなど本文が変わらない `MESSAGE_UPDATE` では `source_content_snapshot` と比較して再翻訳をスキップします。

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
  <target_language>ja</target_language>
  <discord_context>
    <server_name>My Server</server_name>
    <server_overview>...</server_overview>
    <channel_name>general</channel_name>
    <channel_topic>...</channel_topic>
  </discord_context>
  <recent_context>
    <message>
      <author>123456789</author>
      <language>en</language>
      <content>Hello!</content>
    </message>
  </recent_context>
  <final_message>How are you?</final_message>
</translation_request>
```

- **すべてのユーザーコンテンツは XML エスケープされています。** `<`, `>`, `&` 等が含まれていても安全です。
- システムインストラクションはコンテンツを「信頼できない」として扱うよう明示的に指示しています。
- Gemini の temperature は `0.2`（低め）に設定して一貫性を優先しています。

---

## 7. テストの構造

`go test ./...` で全テストを実行できます。

### テストの設計方針

- `Store` のテストはインメモリ SQLite（`":memory:"`）を使用
- `Service` のテストは `fakeDiscordAPI` と `echoTranslator`（入力をそのまま返す）で Discord API と Gemini を差し替え
- `Translator` のテストはプロンプト構造と XML エスケープの正確性を検証

### モックの場所

| テスト対象 | モック実装 |
|---|---|
| Discord API | `service_test.go` の `fakeDiscordAPI` |
| 翻訳エンジン | `service_test.go` の `echoTranslator` |
| HTTP クライアント | `url_alt_test.go` / `avatar_test.go` のインライン `httptest` |

### テストで確認されていないこと

- スラッシュコマンド応答の完全なフロー（`commands_test.go` はオプション解析のみ）
- 実際の Gemini API レスポンス
- 実際の Discord API との通信

---

## 8. ウェブフック名のサニタイズ

Discord の規約により、ウェブフック名に "discord" を含めることが禁止されています。`sanitizeWebhookName` がこれを処理します：

- `"discord"` → `"D-scord"` (大文字小文字問わず)
- 名前が 80 文字を超える場合は切り詰め
- 空白になった場合は `"Gemini Auto Translator"` にフォールバック

ユーザー名にニックネームや表示名が使われるため、`discord` を含むユーザー名は自動的に変換されます。

---

## 9. 設定 (`config.go`) の詳細

| 環境変数 | 必須 | デフォルト | 説明 |
|---|---|---|---|
| `DISCORD_TOKEN` | 必須 | — | ボットのトークン |
| `GEMINI_API_KEY` | 必須 | — | Gemini API のキー |
| `DB_PATH` | 任意 | `./translator.db` | SQLite ファイルのパス |
| `HTTP_ADDR` | 任意 | `:8080` | アバターバッジ HTTP サーバーのアドレス |
| `PUBLIC_BASE_URL` | 任意 | `""` | アバターバッジ URL のベース（末尾スラッシュなし） |

`.env` ファイルの読み込みは **すでに設定されている環境変数を上書きしません**。これにより systemd の `EnvironmentFile` と `.env` が共存できます。

---

## 10. DB スキーマのマイグレーション

`store.Init` で `CREATE TABLE IF NOT EXISTS` を使用しているため、既存 DB に対して安全に実行できます。

カラム追加は以下のように `ALTER TABLE` を `Init` 内で呼び出しています：

```go
// store.go - thread_links に source_channel_id を追加（エラーは無視）
_, _ = s.db.ExecContext(ctx, `ALTER TABLE thread_links ADD COLUMN source_channel_id TEXT NOT NULL DEFAULT ''`)
```

エラーは無視されるため（カラムが既に存在する場合のエラーを許容）、初期バージョンから移行する場合も安全に動作します。

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
- `Service.httpClient` は `http.DefaultClient` を共有します。
- それ以外に共有状態はなく、各イベントハンドラは独立して実行されます。

---

## 13. エラーハンドリングの方針

- すべてのエラーは `main.go` の各ハンドラで `log.Printf` してから処理を継続します（ボットは落ちません）。
- `service.go` 内では最初のエラーで即時 `return` します。途中で失敗した場合、成功したチャンネルへの投稿はロールバックされません。
- Discord API エラー（レート制限・ネットワーク障害など）はリトライしません。

---

## 14. 既知の制約・注意事項

### メッセージ内容がない場合

`HandleMessageCreate` は `Content` が空のメッセージをスキップします（例: 画像のみの投稿）。添付ファイルや埋め込みは翻訳・ミラーリングされません。

### ウェブフック由来メッセージの無視

ボット自身がウェブフックで投稿したメッセージに対してイベントが発火しても、`WebhookID != ""` のチェックでスキップされます。無限ループにはなりません。

### グループ解散時のウェブフック削除なし

`/leave-channel` や `/delete-group` を実行しても、Discord 側に作成されたウェブフックは削除されません。不要なウェブフックが残り続けます（Discord の制限: 1チャンネルあたり最大15個）。

### 同一チャンネルを複数グループに登録可能

1つのチャンネルが複数の翻訳グループに参加できます。その場合、メッセージはすべてのグループのチャンネルへ翻訳・投稿されます。

### スレッドアーカイブ

Discord がスレッドをアーカイブした場合の挙動は考慮されていません。アーカイブ済みスレッドへのウェブフック送信は Discord API エラーになります。

### `translationHistoryLimit` と `translationHistoryMaxAge`

```go
const translationHistoryLimit = 3
const translationHistoryMaxAge = 24 * time.Hour
```

翻訳文脈として使用する直近メッセージ数と時間窓はハードコードされています。必要に応じて設定可能にする余地があります。
