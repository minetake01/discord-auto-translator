# 機能仕様書 — Discord Auto Translator

## 1. プロジェクト概要

Discord Auto Translator は、**複数の言語チャンネルをリンクして自動翻訳・ミラーリングするDiscordボット**です。

あるチャンネルにメッセージが投稿されると、ボットがそのメッセージを Google Gemini API で翻訳し、同じ「翻訳グループ」に属する他のチャンネルへウェブフックで投稿します。投稿者の名前とアバターを偽装できるため、ユーザーには自分の言語でネイティブに会話しているように見えます。

**技術スタック:**

| 要素 | 内容 |
|---|---|
| 言語 | Go 1.24 |
| Discord ライブラリ | `github.com/bwmarrin/discordgo` v0.29.0 |
| 翻訳エンジン | Google Gemini API (`gemini-3.1-flash-lite`) |
| データストア | SQLite (`modernc.org/sqlite` v1.38.2、CGO不要) |
| オプション HTTP サーバー | アバター画像バッジ用 |

---

## 2. 中心概念

### 翻訳グループ (Translation Group)

複数のチャンネルをまとめる論理的な単位です。グループ内の各チャンネルは**異なる BCP-47 言語コード**をひとつ持ちます（同じ言語を複数登録することはできません）。

```
翻訳グループ「general」
  ├── #general-ja  (ja)
  ├── #general-en  (en)
  └── #general-zh  (zh-CN)
```

`#general-ja` にメッセージが来ると、`en` と `zh-CN` に翻訳されてそれぞれのチャンネルへウェブフック投稿されます。

### ウェブフック (Webhook)

各チャンネル登録時にボットが Discord ウェブフックを自動作成し、その ID とトークンを DB に保存します。翻訳メッセージの投稿・編集・削除はすべてこのウェブフックを通じて行われます。

### メッセージリンク (Message Link)

ソースメッセージ ID ↔ ターゲットメッセージ ID の対応を SQLite に保存します。編集・削除・リアクション・リプライ引用の際にこのリンクを辿ります。

---

## 3. 機能一覧

### 3.1 スラッシュコマンド（管理者設定）

すべてのコマンドはサーバー単位で登録されます。レスポンスはエフェメラル（実行者にのみ見える）です。

**権限**: 管理用スラッシュコマンドは `default_member_permissions` を Administrator に設定し、デフォルトではサーバー管理者のみ実行可能にします。追加のロールやメンバーへの許可は、Discord の「サーバー設定」→「連携サービス」→対象 Bot の「管理」→「コマンド権限」で設定します。Bot はロールやコマンド権限を自動変更せず、ハンドラでも独自の権限判定を行いません。メッセージメニューの「View Original」はこの制限の対象外です。

| コマンド | 説明 |
|---|---|
| `/new-channel language:[言語] [channel:[チャンネル]] [group:[グループ名]]` | 翻訳グループを新規作成して最初のチャンネルを登録する |
| `/join-channel group:[グループ] language:[言語] [channel:[チャンネル]]` | 既存グループに追加チャンネルを参加させる |
| `/leave-channel group:[グループ] [channel:[チャンネル]]` | グループからチャンネルを退出させる |
| `/delete-group group:[グループ]` | グループ全体を削除する |
| `/set-style group:[グループ] [preset:[プリセット]] [custom:[カスタム指示]]` | 翻訳グループの翻訳スタイルを設定する（プリセットまたはカスタム指示は排他） |
| `/add-glossary term:[用語] translation:[訳]` | サーバー用語集に優先訳を登録する |
| `/list-glossary` | サーバーの用語集を一覧表示する |
| `/remove-glossary term:[用語]` | 用語集エントリを削除する |

- `language` と `group` オプションはオートコンプリートに対応しています。
- `channel` を省略した場合、コマンドを実行したチャンネルが対象になります。
- 対応チャンネルタイプ: テキスト・ニュース・フォーラム・メディア
- 用語集はサーバーごとに最大 10 件まで登録可能

#### 翻訳スタイル（グループ単位）

`/set-style` でグループごとに翻訳のトーン・文体を調整できます。プリセットとカスタム指示は排他で、同時に指定できません。

| プリセット | 説明 |
|---|---|
| `default` | スタイル指定なし（リセット） |
| `formal` | 丁寧・格式ある文体 |
| `casual` | 友達同士の会話のようなカジュアルな文体 |
| `business` | ビジネス向けの簡潔で礼儀正しい文体 |
| `literal` | できるだけ直訳に近い文体 |
| `gaming` | ゲームコミュニティ向けのカジュアルな文体 |
| `friendly` | 温かく親しみやすい文体 |
| `netslang` | 匿名掲示板・スレ風の文体 |
| `tweet` | SNS（Twitter/X）のつぶやき風の文体 |

- `custom` には自然言語で最大 200 文字までの指示を指定できます。
- `/list-groups` で各グループの現在のスタイルを確認できます。

#### 言語コード

BCP-47 形式 (`en`, `ja`, `zh-CN`, `pt-BR` など) を使用します。`languages.go` に定義されたオートコンプリート候補が表示されますが、候補以外のコードも入力可能です。

### 3.2 メッセージのミラーリング

#### 通常メッセージ

1. `MESSAGE_CREATE` イベントを受信
2. ボットやウェブフック由来のメッセージはスキップ
3. 翻訳グループに属するかチェック
4. 各対象チャンネルへ翻訳して投稿
5. メッセージリンクを DB に保存

投稿時は送信者の表示名とアバター画像をウェブフックのユーザー名・アバターに設定します。TTS フラグはミラー先にも引き継ぎます。添付ファイルとステッカーは再アップロードせず、署名クエリを除いた Discord CDN URL を本文末尾へ追加します。

空白、HTTP(S) URL、Discord メンション、カスタム絵文字、コードブロック、インラインコードだけで構成される本文は、翻訳対象テキストがないものとして Gemini API と翻訳用レート制限を使用せず、原文をミラーリングします。URL 代替版検索など Gemini 以外の処理は通常どおり実行します。

#### メッセージ編集

1. `MESSAGE_UPDATE` イベントを受信
2. DB のメッセージリンクを参照
3. `source_content_snapshot` と本文が同一なら再翻訳をスキップ（ピン留めなど本文不変の更新を除外）
4. 変更がある場合のみ各対象チャンネルのウェブフックメッセージを編集し、snapshot を更新

#### メッセージ削除

1. `MESSAGE_DELETE` イベントを受信
2. DB のメッセージリンクを参照
3. 各対象チャンネルのウェブフックメッセージを削除

### 3.3 リプライ（返信）

返信元メッセージの引用スニペット（最大 40 文字）を先頭に付与します。この Bot が生成する次の2行を「疑似リプライ」と呼びます。

```
> 元のメッセージのスニペット...
-# [original message](https://discord.com/channels/...)
翻訳されたメッセージ本文
```

返信元著者へのメンションは追加しません。

返信元が `message_links` に存在する場合、ミラー先にある転送済みメッセージを Discord API から取得し、疑似リプライ部分を除いた最初の非空行をそのまま引用します。取得に失敗した場合は `source_content_snapshot` の原文を引用します。原文も空の場合は引用を付けず、翻訳済みの返信本文だけを送信します。

`message_links` に対応がない場合も、ゲートウェイの `referenced_message` に含まれる原文をそのまま引用します。引用スニペットには翻訳、用語集、スタイル指定、翻訳レート制限、リンク置換を適用しません。疑似リプライは先頭の `>` 行に上記の `original message` リンク行が続く場合だけ認識するため、通常本文として記述された Markdown 引用は保持されます。
### 3.4 ピン留め同期

1. `MESSAGE_UPDATE` で `pin_states` に保存済みのピン状態と比較
2. 変化時のみ `SyncPin` で全ペアメッセージをピン留め/解除
3. ソース・ミラー双方の Webhook メッセージ更新にも対応（双方向同期）
4. 同期後に全ペアの `pin_states` を更新し、bot 自身のピン操作エコーを抑止

### 3.5 リアクション同期

1. `MESSAGE_REACTION_ADD` / `REMOVE` イベントを受信
2. ボット自身のリアクションはスキップ
3. DB のメッセージリンクを双方向検索
4. すべてのペアメッセージに同じリアクションを追加・削除

### 3.6 スレッド同期

スレッド作成・名前変更・削除を対象チャンネルに同期します。

| ケース | 動作 |
|---|---|
| テキスト/ニュースチャンネルのメッセージから作成されたスレッド | ターゲットにも同じ親メッセージから `CreateThreadFromMessage` で作成。親メッセージリンクが未存在の場合は `THREAD_STARTER_MESSAGE` まで遅延 |
| スタンドアロンスレッド（メッセージなし） | ターゲットに `ThreadStart` で作成 |
| フォーラム/メディアポスト | タイトルと本文を翻訳して `ForumThreadStart` で作成 |
| フォーラムソース → テキストターゲット | テキストターゲットにスレッドを作成後、ウェブフックで最初のメッセージを送信 |
| スレッド内メッセージ | ウェブフック実行時に `thread_id` を指定して対応スレッドへ投稿 |
| スレッド名変更 | 翻訳した名前で対象スレッドを `EditThread` |
| スレッド削除 | 対象スレッドを `DeleteThread` し DB のリンクを削除 |

スレッド作成処理は `sync.Mutex` でシリアライズされ、重複作成を防ぎます。

### 3.7 翻訳品質のための文脈収集

各翻訳リクエストに以下の文脈情報が含まれます：

| 情報 | 取得元 | 用途 |
|---|---|---|
| サーバー名・説明 | Discord API | ドメイン特有の語彙を翻訳に反映 |
| チャンネル名・トピック | Discord API | チャンネルのトーンや主題を翻訳に反映 |
| 直近の会話履歴（最大3件、24時間以内） | DB の `source_content_snapshot` | 会話の流れを踏まえた翻訳 |

### 3.8 URL の代替版置換

翻訳後のテキスト中に含まれる URL に対して、対象言語の `hreflang` 代替 URL が存在する場合に自動置換します（例: `example.com/en` → `example.com/ja`）。

- HTML ページを GET し、`<link rel="alternate" hreflang="...">` タグを参照
- 512 KB までのレスポンスのみ処理
- 応答が遅い場合や失敗した場合はスキップ（best-effort）

### 3.9 Discord リンク・メンション置換

翻訳後のテキスト中に含まれる Discord チャンネル/メッセージ URL および `<#チャンネルID>` メンションについて、参照先が翻訳グループの管理対象であれば、ミラー先言語に対応するチャンネル・メッセージ・スレッド ID に自動置換します。

| 形式 | 置換条件 |
|---|---|
| `https://discord.com/channels/{guild}/{channel}` | `channel` が登録チャンネルまたは同期済みスレッド |
| `https://discord.com/channels/{guild}/{channel}/{message}` | 上記に加え `message_links` に対応が存在 |
| `<#channelId>` | `channelId` が登録チャンネルまたは同期済みスレッド |

- 同一ギルド内のリンクのみ対象（URL 内の `guild` が処理中ギルドと一致する場合）
- メッセージ対応が DB に無い場合は URL を変更しない（壊れたジャンプリンクを避ける）
- 未登録チャンネル・別ギルドのリンクはそのまま
- 外部 URL の `hreflang` 置換（3.8）の後に適用

### 3.10 アバターバッジ（オプション）

`PUBLIC_BASE_URL` を設定すると、ウェブフック送信時のアバター画像 URL が `/avatar?url=...` 経由に置き換えられます。

このエンドポイントが返す画像は元のアバターにオレンジ色の円形ボーダー（128×128 px PNG）を付加したものです。異なる言語のチャンネルで同じユーザーのメッセージを視覚的に区別する助けになります。

### 3.11 用語集 (Glossary)

サーバー単位でソース用語と優先訳を登録し、翻訳プロンプトの `<glossary>` セクションに渡します。

- `/add-glossary` で登録、`/list-glossary` で一覧、`/remove-glossary` で削除
- サーバーごとに最大 10 件

---

## 4. プロンプトインジェクション対策

翻訳プロンプトには以下の対策が施されています。

### プレースホルダー保護 (`placeholders.go`)

翻訳前に以下の要素を `__DAT_KEEP_<hex>__` 形式のプレースホルダーに置き換え、翻訳後に復元します：

- URL (`http://`, `https://`)
- ユーザーメンション (`<@...>`)
- チャンネル・ロールメンション (`<#...>`, `<@&...>`)
- カスタム絵文字 (`<:name:id>`, `<a:name:id>`)
- コードブロック (`` ``` ``, `` ` ``)

スポイラー (`||...||`) はマスクせず、マーカーを保持したまま内側のテキストを翻訳します。

### XML エスケープ

プロンプト内のすべてのユーザーコンテンツ（チャンネル名・サーバー説明・メッセージ本文など）は XML エスケープされます。

### システムプロンプト設計

- すべての `<discord_context>` や `<recent_context>` 内コンテンツを「信頼できないDiscordのコンテンツ」として扱うよう指示
- 翻訳先言語の変更・コード出力・要約などの指示を無視するよう指示

---

## 5. データモデル

### テーブル構成

```sql
-- 翻訳グループ
translation_groups (
    id TEXT,           -- グループID（チャンネル名がデフォルト）
    guild_id TEXT,     -- サーバーID
    display_name TEXT,
    created_by TEXT,   -- 作成者のユーザーID
    created_at TEXT    -- RFC3339Nano
    PRIMARY KEY (guild_id, id)
)

-- グループに参加しているチャンネル
group_channels (
    group_id TEXT,
    guild_id TEXT,
    channel_id TEXT,
    channel_type INTEGER,  -- Discordのチャンネルタイプ定数
    language TEXT,         -- BCP-47
    webhook_id TEXT,
    webhook_token TEXT,
    PRIMARY KEY (group_id, guild_id, channel_id),
    UNIQUE (group_id, guild_id, language)  -- 同グループ内で言語重複禁止
)

-- メッセージの対応関係
message_links (
    source_message_id TEXT,
    source_channel_id TEXT,
    group_id TEXT,
    target_channel_id TEXT,
    target_message_id TEXT,
    target_language TEXT,
    source_author_id TEXT,
    source_content_snapshot TEXT,  -- 翻訳文脈・引用スニペット用
    PRIMARY KEY (source_message_id, source_channel_id, target_channel_id)
)

-- スレッドの対応関係
thread_links (
    group_id TEXT,
    source_thread_id TEXT,
    source_channel_id TEXT,
    target_thread_id TEXT,
    target_channel_id TEXT,
    target_language TEXT,
    PRIMARY KEY (group_id, source_thread_id, target_channel_id)
)

-- ピン留め状態（エコー防止用）
pin_states (
    channel_id TEXT,
    message_id TEXT,
    pinned INTEGER,
    PRIMARY KEY (channel_id, message_id)
)

-- 未使用（将来実装用）
processed_events (event_id, created_at)
glossary_entries (guild_id, source_term, source_term_key, preferred_translation, created_by, created_at)
```

---

## 6. イベント処理フロー

```
Discordゲートウェイ
        │
        ├── InteractionCreate (スラッシュコマンド)
        │       └── CommandHandler.Handle
        │               ├── /new-channel  → Store.CreateGroupWithChannel
        │               ├── /join-channel → Store.JoinChannel
        │               ├── /leave-channel → Store.LeaveChannel
        │               ├── /delete-group → Store.DeleteGroup
        │               ├── /set-style → Store.SetGroupStyle
        │               ├── /add-glossary → Store.UpsertGlossaryEntry
        │               ├── /list-glossary → Store.ListGlossaryEntries
        │               └── /remove-glossary → Store.RemoveGlossaryEntry
        │
        ├── MessageCreate
        │       ├── ボット/ウェブフック → スキップ
        │       ├── ThreadStarterMessage → ensureThreadSynced のみ
        │       ├── ensureThreadSynced（スレッド内の初回メッセージ）
        │       ├── handleThreadMessageCreate（スレッド内メッセージ）
        │       └── 通常メッセージ → Translate → SendWebhook → SaveMessageLink
        │
        ├── MessageUpdate → Translate → EditWebhook
        ├── MessageDelete → DeleteWebhook
        ├── MessageReactionAdd/Remove → SyncReaction
        ├── ThreadCreate → SyncThreadCreateFromGateway
        ├── ThreadUpdate → SyncThreadUpdate（名前変更のみ）
        └── ThreadDelete → SyncThreadDelete
```

---

## 7. HTTP サーバー

ボット起動時に `HTTP_ADDR`（デフォルト `:8080`）でHTTPサーバーが起動します。

| エンドポイント | 説明 |
|---|---|
| `GET /avatar?url=<エンコード済みURL>` | 元のアバター画像にオレンジリングを追加した PNG を返す |

`PUBLIC_BASE_URL` が未設定の場合、このエンドポイントは機能しますがウェブフックからは参照されません。

---

## 8. デプロイ

### ローカル実行

```sh
cp .env.example .env
# .env を編集して TOKEN と API KEY を設定
go run ./cmd/discord-auto-translator
```

### GCE デプロイ (`deploy/deploy-gce.ps1`)

Windows PowerShell から実行するスクリプトで、以下の処理を行います：

1. `go test ./...` を実行（`-SkipTests` で省略可）
2. `linux/amd64` バイナリをクロスコンパイル（CGO_ENABLED=0）
3. `-Bootstrap` フラグ時: GCE インスタンスに Caddy + systemd ユニットをセットアップ
4. バイナリをインスタンスへ転送・配置
5. `-UploadEnv` フラグ時: `.env` も転送
6. `-UploadDb` フラグ時: `translator.db` も転送
7. systemd サービスを再起動

```powershell
# 初回（インフラセットアップ含む）
.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv

# 以降のコード更新
.\deploy\deploy-gce.ps1
```

デフォルトのデプロイ先: `discord-translator.minetake.net`（`$Domain` パラメータで変更可）
