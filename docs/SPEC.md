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

すべてのコマンドはサーバー単位で登録されます。レスポンスはエフェメラル（実行者にのみ見える）で、実行者の Discord クライアントの言語設定（`interaction.Locale`）に合わせてローカライズされます（未対応言語は英語）。エラーメッセージも同様にローカライズされ、内部エラーの詳細はユーザーへ表示せずログにのみ記録されます。

**権限**: 管理用スラッシュコマンドは `default_member_permissions` を Administrator に設定し、デフォルトではサーバー管理者のみ実行可能にします。追加のロールやメンバーへの許可は、Discord の「サーバー設定」→「連携サービス」→対象 Bot の「管理」→「コマンド権限」で設定します。Bot はロールやコマンド権限を自動変更せず、ハンドラでも独自の権限判定を行いません。メッセージメニューの「View Original」はこの制限の対象外です。

| コマンド | 説明 |
|---|---|
| `/new-channel language:[言語] [channel:[チャンネル]] [group:[グループ名]]` | 翻訳グループを新規作成して最初のチャンネルを登録する |
| `/join-channel group:[グループ] language:[言語] [channel:[チャンネル]]` | 既存グループに追加チャンネルを参加させる |
| `/leave-channel group:[グループ] [channel:[チャンネル]]` | グループからチャンネルを退出させる |
| `/delete-group group:[グループ]` | グループ全体を削除する |
| `/set-style group:[グループ] [preset:[プリセット]] [custom:[カスタム指示]]` | 翻訳グループの翻訳スタイルを設定する（プリセットまたはカスタム指示は排他） |
| `/add-glossary term:[用語] translation:[訳] attribute:[属性] always_include:[常時使用]` | サーバー用語集に優先訳を登録する（`attribute` は候補付き自由入力、`always_include` の既定値は `false`） |
| `/list-glossary` | サーバーの用語集を一覧表示する |
| `/remove-glossary term:[用語]` | 用語集エントリを削除する |

- `language` と `group` オプションはオートコンプリートに対応しています。
- `channel` を省略した場合、コマンドを実行したチャンネルが対象になります。
- 対応チャンネルタイプ: テキスト・ニュース・フォーラム・メディア
- 用語集はサーバーごとに最大 50 件まで登録可能

#### 翻訳スタイル（グループ単位）

`/set-style` でグループごとに翻訳スタイルを設定できます。スタイルは原文のトーンを上書きする命令ではなく、原文だけでは決まらない選択（敬語の有無など、ターゲット言語が強制する区別）を解決するためのデフォルトとして翻訳モデルに渡されます。プリセットとカスタム指示は排他で、同時に指定できません。

| プリセット | 説明 |
|---|---|
| `default` | スタイル指定なし（リセット） |
| `formal` | 丁寧・格式ある文体をデフォルトにする |
| `casual` | 友達同士の会話のようなカジュアルな文体をデフォルトにする |
| `business` | ビジネス向けの簡潔で礼儀正しい文体をデフォルトにする |
| `literal` | 訳し方が複数あるとき最も直訳に近いものを選ぶ |
| `gaming` | ゲームコミュニティ向けのカジュアルな文体をデフォルトにする |
| `friendly` | 温かく親しみやすい文体をデフォルトにする |
| `netslang` | 匿名掲示板・スレ風の文体をデフォルトにする |
| `tweet` | SNS（Twitter/X）のつぶやき風の文体をデフォルトにする |

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

翻訳に失敗した場合、またはギルド単位の Gemini トークンレート制限（`GEMINI_RATE_LIMIT_TOKENS_PER_MIN`）を超過した場合はミラーリングせず、投稿元チャンネルへ通知を投稿します。通知文言は投稿元チャンネルに登録された言語で表示されます。

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

返信元メッセージの引用スニペット（最大 40 文字）を先頭に付与します。この Bot が生成する次の形式を「疑似リプライ」と呼びます。

```
> 元のメッセージのスニペット... · [Source](https://discord.com/channels/...)

メッセージ本文
```

返信元著者へのメンションは追加しません。

返信元が `message_links` に存在する場合、ミラー先にある転送済みメッセージを Discord API から取得し、疑似リプライ部分を除いた最初の非空行を引用します。取得に失敗した場合は `source_content_snapshot` の原文を引用します。原文も空の場合は引用を付けず、翻訳済みの返信本文だけを送信します。

`message_links` に対応がない場合も、ゲートウェイの `referenced_message` に含まれる原文を引用します。引用スニペットが Markdown ATX ヘッダー行（`# ` で始まる行）の場合は、先頭の `#` を除去して Discord の subtext 形式 `-# ` に正規化してから引用します。それ以外の行はそのまま引用します。引用スニペットには翻訳、用語集、スタイル指定、翻訳レート制限、リンク置換を適用しません。リンクラベルは送信先チャンネルの言語に合わせ、未対応言語では英語の `Source` を使用します（日本語は `引用元を見る`）。疑似リプライは先頭の `>` 行にスニペット、半角スペース、ミドルドット、半角スペース、Discord メッセージリンクが並ぶ場合だけ認識するため、通常本文として記述された Markdown 引用は保持されます。疑似リプライと返信本文の間には空行を1行置きます。

### 3.4 転送メッセージ

Discord の `message_reference.type=FORWARD` と作成時点で不変の `message_snapshots[0]` を使用し、返信とは別の経路で転送メッセージをミラーします。snapshot が1件でない、または `message` が `null` のペイロードはエラーとして扱い、通常メッセージや返信にはフォールバックしません。

```
-# Forwarded · https://discord.com/channels/...
メッセージ本文
```

見出しは送信先言語に合わせてローカライズし（日本語は `転送済み`）、Discord メッセージURLはリンクラベルを付けず直接表示します。見出しと本文の間に空行は入れません。

参照元が `message_links` に存在し、送信先チャンネルに対応するミラーを取得できる場合は、その翻訳済み本文とミラー先URLを再利用して Gemini API を呼びません。取得した本文の先頭に Bot が生成した疑似返信または転送見出しがある場合は、その見出しだけを除去します。送信先に対応するミラーがない場合は snapshot 本文だけを翻訳し、参照元URLを表示します。翻訳対象文字がない本文では API を呼びません。

snapshot の添付ファイルとステッカーは通常メッセージと同じURL化処理で保持します。embed と component は対象外です。保存する `source_content_snapshot` には転送イベント外側の空本文ではなく snapshot 本文を使用します。

### 3.5 ピン留め同期

1. `MESSAGE_UPDATE` で `pin_states` に保存済みのピン状態と比較
2. 変化時のみ `SyncPin` で全ペアメッセージをピン留め/解除
3. ソース・ミラー双方の Webhook メッセージ更新にも対応（双方向同期）
4. 同期後に全ペアの `pin_states` を更新し、bot 自身のピン操作エコーを抑止

### 3.6 リアクション同期

1. `MESSAGE_REACTION_ADD` / `REMOVE` イベントを受信
2. ボット自身のリアクションはスキップ
3. DB のメッセージリンクを双方向検索
4. すべてのペアメッセージに同じリアクションを追加・削除

### 3.7 スレッド同期

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

### 3.8 翻訳品質のための文脈収集

各翻訳リクエストに以下の文脈情報が含まれます：

| 情報 | 取得元 | 用途 |
|---|---|---|
| サーバー名・説明 | Discord API | ドメイン特有の語彙を翻訳に反映 |
| チャンネル名・トピック | Discord API | チャンネルのトーンや主題を翻訳に反映（スレッド内メッセージでは親チャンネル） |
| スレッド名 | Gateway イベントまたは Discord API | スレッド内メッセージの翻訳時に `<thread_name>` としてスレッドの主題を反映 |
| 直近の会話履歴（最大3件、24時間以内） | 翻訳グループ内の全チャンネル（または同期済みスレッド）の DB `source_content_snapshot` | 会話の流れを踏まえた翻訳。各メッセージは原文スナップショットと投稿者表示名（`author`）付き |
| リプライ引用チェイン（最大3件、時間制限なし） | `message_links` による原文解決 + Discord API で参照を遡る | `<recent_context>` より優先して、返信先メッセージの原文を解釈に利用。各メッセージは `author` 付き |
| 翻訳対象メッセージの投稿者 | 処理中 `DiscordMessage.AuthorDisplayName` | `<final_message author="...">` として翻訳対象の話者を明示 |

### 3.9 URL の代替版置換

翻訳後のテキスト中に含まれる URL に対して、対象言語の `hreflang` 代替 URL が存在する場合に自動置換します（例: `example.com/en` → `example.com/ja`）。

- HTML ページを GET し、`<link rel="alternate" hreflang="...">` タグを参照
- 512 KB までのレスポンスのみ処理
- 応答が遅い場合や失敗した場合はスキップ（best-effort）

### 3.10 Discord リンク・メンション置換

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

### 3.11 アバターバッジ（オプション）

`PUBLIC_BASE_URL` を設定すると、ウェブフック送信時のアバター画像 URL が `/avatar?url=...&color=...` 経由に置き換えられます。

このエンドポイントが返す画像は元のアバターに円形ボーダー（128×128 px PNG）を付加したものです。リング色は投稿者の最上位（Position 最大）の色付きロール色に合わせます。色付きロールがない場合はニュートラルグレー（`#72767D`）を使います。

### 3.12 用語集 (Glossary)

サーバー単位でソース用語と優先訳を登録し、条件を満たす用語を翻訳のシステム指示にある `<glossary>` セクションへ渡します。

- `/add-glossary` で登録、`/list-glossary` で一覧、`/remove-glossary` で削除
- `attribute` は任意の自由入力。Autocomplete候補として「人名」「地名」「スラング」「略語」「専門用語」を提示し、選択を強制しない
- 属性は選別された用語の `<attribute>` としてシステム指示へ渡し、Geminiが用語の意味・役割を判断する文脈として使用する
- `always_include:false`（既定値）の用語は、現在の翻訳対象本文に `term` が大文字・小文字を無視して含まれる場合だけ追加
- `always_include:true` の用語は本文にかかわらず常に追加
- 一致判定の対象は現在の翻訳対象本文だけで、会話履歴やサーバー・チャンネル情報は対象外
- サーバーごとに最大 50 件

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

- すべての `<discord_context>`、`<recent_context>`、`<reply_context>`、`<final_message>` 内コンテンツを「信頼できないDiscordのコンテンツ」として扱うよう指示
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
    created_at INTEGER, -- Unix milliseconds
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
    source_message_id INTEGER, -- Discord snowflake
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
    message_id INTEGER, -- Discord snowflake
    pinned INTEGER,
    PRIMARY KEY (channel_id, message_id)
)

-- 未使用（将来実装用）
processed_events (event_id, created_at INTEGER) -- Unix milliseconds
glossary_entries (guild_id, source_term, source_term_key, preferred_translation, attribute, always_include, created_by, created_at INTEGER) -- Unix milliseconds
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
| `GET /avatar?url=<エンコード済みURL>&color=<6桁HEX>` | 元のアバター画像にロール色（またはデフォルトグレー）のリングを追加した PNG を返す |

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

1. `deploy/deploy.json` から GCE 接続先を読み込む
2. `go test ./...` を実行（`-SkipTests` で省略可）
3. `linux/amd64` バイナリをクロスコンパイル（CGO_ENABLED=0）
4. `-Bootstrap` フラグ時: 指定 env の `PUBLIC_BASE_URL` / `HTTP_ADDR` で Caddy + systemd をセットアップ
5. バイナリをインスタンスへ転送・配置
6. `-UploadEnv` フラグ時: 指定 env をサーバー上の `.env` として配置
7. `-UploadDb` フラグ時: `translator.db` も転送
8. systemd サービスを再起動

設定は2ファイルに分離します：

| ファイル | 内容 |
|---|---|
| `deploy/deploy.json` | インスタンス名・ゾーン・SSH ユーザー等のインフラ設定 |
| `.env`（デフォルト） | `DISCORD_TOKEN`・`GEMINI_API_KEY`・`PUBLIC_BASE_URL` 等 |

env ファイルの解決順序: `-EnvFile` 引数 > `deploy.json` の `envFile` > `.env`

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# deploy.json と .env を編集

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # 初回
.\deploy\deploy-gce.ps1                          # コード更新のみ
.\deploy\deploy-gce.ps1 -UploadEnv               # シークレット更新
.\deploy\deploy-gce.ps1 -EnvFile .env.staging -UploadEnv  # 一時的な上書き
```

本番用に別 env を使う場合は `cp .env.example .env.production` のように作成し、`deploy.json` の `"envFile": ".env.production"` に設定します。`-UploadEnv` なしのデプロイではサーバー上の既存 `.env` がそのまま使われます。
