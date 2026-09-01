# 機能仕様書 — Discord Auto Translator

## 1. プロジェクト概要

Discord Auto Translator は、**複数の言語チャンネルをリンクして自動翻訳・ミラーリングするDiscordボット**です。

あるチャンネルにメッセージが投稿されると、ボットがそのメッセージを OpenAI 互換 Chat Completions API（`OPENAI_MODEL`）で翻訳し、同じ「翻訳グループ」に属する他のチャンネルへウェブフックで投稿します。投稿者の名前とアバターを偽装できるため、ユーザーには自分の言語でネイティブに会話しているように見えます。

**技術スタック:**

| 要素 | 内容 |
|---|---|
| 言語 | Go 1.24 |
| Discord ライブラリ | `github.com/bwmarrin/discordgo` v0.29.0 |
| 翻訳エンジン | OpenAI 互換 Chat Completions（`internal/translatorbot/openai.go`） |
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
| `/join-channel group:[グループ] language:[言語] [channel:[チャンネル]]` | 既存グループに追加チャンネルを参加させる。グループ内の既存チャンネルとチャンネルタイプが異なる場合は拒否する（フォーラム／メディアで他にタグ付きピアがある場合はタグ対応付け UI を表示） |
| `/leave-channel group:[グループ] [channel:[チャンネル]]` | グループからチャンネルを退出させる |
| `/delete-group group:[グループ]` | グループ全体を削除する |
| `/edit-forum-tags group:[グループ] [channel:[フォーラム]]` | フォーラム／メディアのタグ対応付けを編集する |
| `/set-style group:[グループ] [preset:[プリセット]] [custom:[カスタム指示]]` | 翻訳グループの翻訳スタイルを設定する（プリセットまたはカスタム指示は排他） |
| `/add-glossary term:[用語] translation:[訳] attribute:[属性] always_include:[常時使用]` | サーバー用語集に優先訳を登録する（`attribute` は候補付き自由入力、`always_include` の既定値は `false`） |
| `/list-glossary` | サーバーの用語集を一覧表示する |
| `/remove-glossary term:[用語]` | 用語集エントリを削除する |

- `language` と `group` オプションはオートコンプリートに対応しています。
- `channel` を省略した場合、コマンドを実行したチャンネルが対象になります。
- 対応チャンネルタイプ: テキスト・ニュース・フォーラム・メディア。グループ内のチャンネルはすべて同じチャンネルタイプでなければならない（テキスト≠ニュース≠フォーラム≠メディア）
- フォーラム／メディアのタグ対応付けは `/join-channel` 成功時（ピアがある場合）および `/edit-forum-tags` のエフェメラル Select Menu で編集する。相手タグで「対応なし」を選んで保存するとその対応を削除する
- 用語集はサーバーごとに最大 50 件まで登録可能

#### 翻訳スタイル（グループ単位）

`/set-style` でグループごとに翻訳スタイルを設定できます。スタイルは原文のトーンを上書きする命令ではなく、原文だけでは決まらない選択（敬語の有無など、ターゲット言語が強制する区別）を解決するためのデフォルトとして翻訳モデルに渡されます。プリセットとカスタム指示は排他で、同時に指定できません。`default` およびカジュアル系プリセット（`casual`・`gaming`・`friendly`・`netslang`・`tweet`）は、ターゲット言語のネイティブがチャットで実際に打つ自然な文体を基本とします。

| プリセット | 説明 |
|---|---|
| `default` | 自然なチャット文体（ネイティブが実際に打つ文体）をデフォルトにする |
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

投稿時は送信者の表示名とアバター画像をウェブフックのユーザー名・アバターに設定します。TTS フラグはミラー先にも引き継ぎます。画像添付は縮小して翻訳モデルへ渡し（文脈）、元バイトを再アップロードします。ダウンロードや縮小に失敗した画像はスキップし、署名クエリを除いた Discord CDN URL を本文末尾へ追加します。ソースに代替テキスト（`attachment.description`）がある場合のみ各言語へ翻訳して付与します（最大 1024 文字）。代替テキストが無い画像には生成せず、空のまま再アップロードします。非画像添付とステッカーは再アップロードせず、署名クエリを除いた Discord CDN URL を本文末尾へ追加します。

空白、HTTP(S) URL、Discord メンション、カスタム絵文字、コードブロック、インラインコードだけで構成される本文でも、翻訳対象の既存代替テキストがある画像添付があれば翻訳 API を呼び出します。翻訳対象テキストも既存代替テキストもない場合は翻訳 API と翻訳用レート制限を使用せず、原文をミラーリングします。URL 代替版検索など翻訳 API 以外の処理は通常どおり実行します。

翻訳に失敗した場合、またはギルド単位の翻訳トークンレート制限（`TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN`、デフォルト `100000`）を超過した場合はミラーリングせず、投稿元メッセージへのリプライとして通知を投稿します（fail-closed）。通知文言は投稿元チャンネルに登録された言語で表示されます。プロバイダの一時障害（タイムアウト・通信エラー・HTTP 429/5xx。リトライ後も失敗した場合）と自前のトークンレート制限では、その状態が解消されるまで投稿元チャンネルごとに最初の1回だけ通知し、制限では制限が解除されるまで、プロバイダ障害では外部の翻訳サービスが復旧するまで、しばらく翻訳されない可能性がある旨を含めます。契約違反はメッセージごとに通知します。

#### メッセージ編集

1. `MESSAGE_UPDATE` イベントを受信
2. DB のメッセージリンクを参照
3. `source_content_snapshot` と本文が同一なら再翻訳をスキップ（ピン留めなど本文不変の更新を除外）
4. 変更がある場合のみ各対象チャンネルのウェブフックメッセージを編集し、snapshot を更新

#### メッセージ削除

1. `MESSAGE_DELETE` イベントを受信
2. DB のメッセージリンクを参照
3. 各対象チャンネルのウェブフックメッセージを削除

#### 投票メッセージ

Discord のネイティブ投票（`message.poll`）は、ネイティブ Poll としてはミラーせず、Webhook Embed 付きの通常メッセージとして投稿します。Webhook 経由のため Bot の `Embed Links` 権限は不要で、既存の招待権限から変更はありません。

1. `MESSAGE_CREATE` で `poll` を受信（本文が空でも投票があれば処理する）
2. 付随本文は従来どおり翻訳し、質問と選択肢は1回の構造化翻訳（言語ごとに `question` と `answers[]`）でまとめて処理する
3. `content` 先頭に送信先言語の案内文を疑似リプライ形式で付与し、元の投票メッセージへのリンクを含める（例: 日本語 `> -# 投票を開始しました。 · [投票する](https://discord.com/channels/...)`）。案内文の直後に空行は入れない
4. Embed の `title` に翻訳済み質問、`description` に番号付き選択肢（回答絵文字はソース側から付与）を載せる。著者のロールカラーがあれば Embed の `color` に使う
5. 投票リンクはソース側メッセージを指し、グループ内の Discord リンク置換の対象外とする（翻訳・置換の後に付与する）
6. `poll.expiry` がある場合、言語ごとの翻訳済み選択肢を `poll_translation_cache` に保存する（`expires_at = poll.expiry`）。`expiry` が無い投票はキャッシュしない

票の同期や投票の早期終了同期は行いません。投票メッセージ自体は Discord 仕様上編集できないため、特別な更新処理はありません。削除は通常メッセージと同様です。疑似リプライ引用のため、ミラー先メッセージ取得時は先頭 Embed の title（なければ description 先頭行）を本文に合成します。

#### 投票終了システム通知

Discord が投票終了後に送る `POLL_RESULT`（message type 46）は専用パスでミラーします。通常の返信としては扱いません。

1. type 46 を検出し、`poll_result` embed の fields（`victor_answer_*` / `total_votes` など）を読む
2. 既存の疑似リプライ（`replyQuote`）で元投票を引用する
3. 本文は翻訳 API を使わず UI 文言のみとする
   - 勝者表示名を解決できた場合: 終了文 + `結果: {選択肢}（{整数%}）`（％は `victor_answer_votes * 100 / total_votes` を四捨五入。分数や総票数の単独表示はしない）
   - embed はあるが勝者が取れない場合（同点など）: 終了文 + `勝者はいませんでした。`
   - embed 自体が使えない場合: 終了文のみ
4. 勝者の選択肢名は送信先言語の `poll_translation_cache` を優先し、欠落時のみ embed の原文 `victor_answer_text` にフォールバックする
5. グループへのミラー処理が終わったら、当該元投票のキャッシュ行を即削除する。結果通知が来なかった孤児は `expires_at` 経過後に定期 purge する

### 3.3 リプライ（返信）

返信元メッセージの引用スニペット（最大 40 文字）を先頭に付与します。この Bot が生成する次の形式を「疑似リプライ」と呼びます。

```
> -# 元のメッセージのスニペット... · [Source](https://discord.com/channels/...)

メッセージ本文
```

返信元著者へのメンションは追加しません。

返信元が `message_links` に存在する場合、ミラー先にある転送済みメッセージを Discord API から取得し、疑似リプライ部分を除いた最初の非空行を引用します。取得に失敗した場合は `source_content_snapshot` の原文を引用します。原文も空の場合は引用を付けず、翻訳済みの返信本文だけを送信します。

`message_links` に対応がない場合も、ゲートウェイの `referenced_message` に含まれる原文を引用します。引用スニペットは Discord の subtext 形式 `-# ` に正規化します。Markdown ATX ヘッダー行（`# ` で始まる行）の場合は、先頭の `#` を除去してから `-# ` を付与します。引用スニペットには翻訳、用語集、スタイル指定、翻訳レート制限、リンク置換を適用しません。リンクラベルは送信先チャンネルの言語に合わせ、未対応言語では英語の `Source` を使用します（日本語は `引用元を見る`）。疑似リプライは先頭の `>` 行にスニペット、半角スペース、ミドルドット、半角スペース、Discord メッセージリンクが並ぶ場合だけ認識するため、通常本文として記述された Markdown 引用は保持されます。疑似リプライと返信本文の間には空行を1行置きます。

疑似リプライの投稿後に返信元が削除された場合は、保存済みの返信参照から既存の疑似リプライを逆引きし、返信本文を保持したまま引用部分を送信先チャンネルの言語で `> -# 元のメッセージが削除されました` に相当する削除表示へ置換します。削除表示にはメッセージリンクを付けません。

### 3.4 転送メッセージ

Discord の `message_reference.type=FORWARD` と作成時点で不変の `message_snapshots[0]` を使用し、返信とは別の経路で転送メッセージをミラーします。snapshot が1件でない、または `message` が `null` のペイロードはエラーとして扱い、通常メッセージや返信にはフォールバックしません。

```
-# Forwarded · https://discord.com/channels/...
メッセージ本文
```

見出しは送信先言語に合わせてローカライズし（日本語は `転送済み`）、Discord メッセージURLはリンクラベルを付けず直接表示します。見出しと本文の間に空行は入れません。

参照元が `message_links` に存在し、送信先チャンネルに対応するミラーを取得できる場合は、その翻訳済み本文とミラー先URLを再利用して翻訳 API を呼びません。取得した本文の先頭に Bot が生成した疑似返信または転送見出しがある場合は、その見出しだけを除去します。送信先に対応するミラーがない場合は snapshot 本文だけを翻訳し、参照元URLを表示します。翻訳対象文字がない本文では API を呼びません。

snapshot の画像添付は通常メッセージと同じく再アップロードし、非画像添付とステッカーは CDN URL を本文末尾へ追加します。embed と component は対象外です。保存する `source_content_snapshot` には転送イベント外側の空本文ではなく snapshot 本文を使用します。

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
| テキスト/ニュースチャンネルのメッセージから作成されたスレッド | ターゲットにも同じ親メッセージから `CreateThreadFromMessage` で作成。親メッセージリンクが未存在の場合は翻訳せず `THREAD_STARTER_MESSAGE` まで遅延 |
| スタンドアロンスレッド（メッセージなし） | ターゲットに `ThreadStart` で作成。Gateway の `THREAD_CREATE` 時点では作成せず、最初の本文で作成する |
| フォーラム/メディアポスト | タイトルと初期本文を1回の翻訳リクエストで翻訳して `ForumThreadStart` で作成。ソースの `applied_tags` は `forum_tag_maps` で対応付けたタグ ID に変換して付与する。未マップのタグは省略。宛先が `REQUIRE_TAG` でマップ結果が空のときはそのターゲット作成を失敗させる。Gateway の `THREAD_CREATE` で初回本文がまだ無いときは翻訳せず遅延する |
| スレッド内メッセージ | ウェブフック実行時に `thread_id` を指定して対応スレッドへ投稿。ミラー先スレッドへの投稿は既存 `thread_links` を逆向きに辿って元スレッドへ戻す。新規スレッドは作らない |
| スレッド名変更 | 翻訳した名前で対象スレッドを `EditThread` |
| フォーラムタグ変更 | `THREAD_UPDATE` の `applied_tags` 差分をマップして対象スレッドへ同期。マップ後の集合が既存と同一なら no-op |
| スレッド削除 | 対象スレッドを `DeleteThread` し DB のリンクを削除 |

スレッド作成処理は `sync.Mutex` でシリアライズされ、重複作成を防ぎます。すでに `thread_links` の source または target として記録されているスレッドでは対向を新規作成しません。スレッド作成時のタイトルと初期本文は、作成が確定してから構造化レスポンス（`name` / `message`）でまとめて翻訳し、投票がある場合だけ別リクエストにします。名前変更はタイトルのみの既存翻訳パスを使います。

### 3.8 翻訳品質のための文脈収集

各翻訳リクエストに以下の文脈情報が含まれます：

| 情報 | 取得元 | 用途 |
|---|---|---|
| サーバー名・説明 | Discord API | ドメイン特有の語彙を翻訳に反映 |
| チャンネル名・トピック | Discord API | チャンネルのトーンや主題を翻訳に反映（スレッド内メッセージでは親チャンネル） |
| スレッド名 | Gateway イベントまたは Discord API | スレッド内メッセージの翻訳時に `<thread_name>` としてスレッドの主題を反映 |
| 直近の会話履歴（同一バースト。沈黙15分・件数16/8・時間幅30/15分・トークン800/400で世代切替） | 翻訳グループ内の全チャンネル（または同期済みスレッド）の DB `source_content_snapshot` と画像添付 | 会話の流れを踏まえた翻訳と、プレフィックスキャッシュ可能な履歴文脈。各メッセージは原文スナップショットと投稿者表示名（`author`）付き。画像添付は縮小してビジョン入力へ（文脈のみ、再投稿しない）。24時間窓は使わない |
| 切り捨て済み話題の要約 | 世代切替で捨てた枠から非同期生成し、会話ロケーションと世代 ID に保存 | 次の翻訳から履歴ユーザーパートの `<topic_summary>` として挿入。件数・時間幅・トークンの世代切替上限には含めない。未完了・失敗時は要約なしで翻訳を続ける |
| リプライ引用チェイン（最大3件、時間制限なし） | `message_links` による原文解決 + Discord API で参照を遡る | `<recent_context>` より優先して、返信先メッセージの原文を解釈に利用。各メッセージは `author` 付き。画像添付も履歴と同様にビジョン入力へ（文脈のみ）。履歴枠からは除去しない（重複可）。末尾枠だけが同一投稿なら除外する |
| 翻訳対象メッセージの投稿者 | 処理中 `DiscordMessage.AuthorDisplayName` | `<final_message author="...">` として翻訳対象の話者を明示 |
| 共有 URL のページメタ（title / description / image） | 本文中の HTTP(S) URL を GET して OGP / Twitter / `<title>` 等から抽出 | `<site_context>` と `[SITE:N]` プレースホルダでリンク先の背景を翻訳に反映。`og:image` / `twitter:image` は縮小してビジョン入力へ（文脈のみ、再投稿しない） |
| 画像添付 | Discord CDN から取得し、最長辺 768px の JPEG に縮小 | 本文翻訳の文脈。既存 alt があるときだけ各言語へ翻訳して再アップロード。alt の生成はしない |

翻訳対象テキストまたは既存の画像代替テキストがあるメッセージでは、翻訳 API 呼び出し前に本文中の URL を best-effort で取得します。Discord 系ホストは取得対象外です。title が取れた URL だけ `<site_context>` に載せ、プレースホルダの `[SITE:N]` と `<site id="N">` で対応付けます。画像添付のダウンロードや縮小に失敗した場合はスキップし、再アップロードできない画像は署名クエリを除いた Discord CDN URL を本文末尾へ追加します（投稿元へは通知しない）。履歴・リプライ・OGP 画像の取得失敗はスキップします。本文が空でも画像添付がある履歴・リプライは文脈に残します。

直近履歴の枠自体は追加テーブルを持たず、各翻訳リクエストで `message_links` から再計算します。隣接投稿の間隔が 15 分を超えたらそれより古い枠は使いません（沈黙で切れたバーストの要約も使いません）。同一バースト内では束ね後枠を append-only に積み、件数が 16 を超える・時間幅が 30 分を超える・推定トークンが 800 を超えるのいずれかで世代を切り、残す枠は 8 件以下かつ幅 15 分以下かつ 400 トークン以下になるまで古い側をまとめて捨てます（1 枠だけが上限を超える場合はその 1 枠を残して翻訳を続けます）。切り捨てが確定した翻訳は待たず、捨てた枠（と前回の要約があればそれ）から話題を短く要約するリクエストを裏で送ります。要約は会話ロケーションと世代 ID に紐づけて保存し、次のメッセージから履歴ユーザーパートの `<topic_summary>` に載せます。要約生成中に次のメッセージが来た場合も待ちません。同一作者の短文マージ（5 分・最大 4 件）は末尾枠にだけ適用し、それより前の枠の本文は世代内で変わりません。ユーザープロンプトは安定パート（`<target_languages>`、style、discord_context、`always_include` glossary）、履歴パート（`<topic_summary>`、`<recent_context>` 全枠、`<reply_context>`）、可変パート（本文マッチ glossary と対象メッセージの site/attachments/attachment_alts/final）に分かれます。`prompt_cache_key` は会話ロケーションで決まり、世代や要約の有無では分けない。履歴プレフィックスの違いは本文の不一致で隔離する。投票・スレッド作成は kind を付けてメッセージ翻訳とキーを分ける。履歴が無いバースト先頭では現在メッセージを世代先頭とする（要約の保存キーであり、`prompt_cache_key` には使わない）。system テキスト、安定ユーザーパート、履歴ユーザーパートのそれぞれに `prompt_cache_breakpoint` を置き、`prompt_cache_options.mode=explicit` を送ります。`prompt_cache_options.ttl` は送らず、キャッシュ寿命はプロバイダーの自動キャッシュに任せます。メッセージ翻訳の system（不変領域）は推定 1200 トークン前後（1500 以下）にサイズし、system ブレークポイントだけで典型プロバイダーのキャッシュ下限（1024）を超えます。メッセージ・投票・スレッド作成・話題要約それぞれのシステムプロンプトはリクエスト間で固定します。JSON Schema は用途ごとに決め、翻訳用途ではリクエストの `<target_languages>` をルートのオブジェクトキーにする（配列は使わない）。メッセージ翻訳では翻訳対象の既存 alt があるときだけ `attachment_descriptions` を required にし、件数を `minItems`/`maxItems` で固定する。`always_include` の glossary は安定ユーザーパート、本文マッチ glossary と現在メッセージの添付・OGP 画像は可変パートです。履歴・リプライの `<image>` タグは履歴パートに置き、ビジョンバイトはテキストパートより後ろ（明示 breakpoint があるときはその後）です。投票・スレッド作成・話題要約は別 system / 別 schema / 別キャッシュキーです。履歴取得や要約の失敗は翻訳自体を止めません。

### 3.9 URL の代替版置換

翻訳後のテキスト中に含まれる URL に対して、対象言語の `hreflang` 代替 URL が存在する場合に自動置換します（例: `example.com/en` → `example.com/ja`）。

- HTML ページを GET し、`<link rel="alternate" hreflang="...">` タグを参照（翻訳前のページメタ取得と同一の URL 単位キャッシュを共有）
- 512 KB までのレスポンスのみ処理
- 応答が遅い場合や失敗した場合はスキップ（best-effort）
- キャッシュは URL 単位（TTL 24h）。リクエスト失敗はキャッシュしない

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

サーバー単位でソース用語と優先訳を登録し、条件を満たす用語を翻訳プロンプトの `<glossary>` セクションへ渡します。

- `/add-glossary` で登録、`/list-glossary` で一覧、`/remove-glossary` で削除
- `attribute` は任意の自由入力。Autocomplete候補として「人名」「地名」「スラング」「略語」「専門用語」を提示し、選択を強制しない
- 属性は選別された用語の `<attribute>` としてプロンプトへ渡し、翻訳モデルが用語の意味・役割を判断する文脈として使用する
- `always_include:false`（既定値）の用語は、現在の翻訳対象本文に `term` が大文字・小文字を無視して含まれる場合だけ、可変ユーザーパートへ追加
- `always_include:true` の用語は本文にかかわらず、安定ユーザーパートへ追加
- 用語の適用方法はシステム指示に固定文として常に含め、用語の実データはシステム指示へ置かない
- 一致判定の対象は現在の翻訳対象本文だけで、会話履歴やサーバー・チャンネル情報は対象外
- サーバーごとに最大 50 件

### 3.13 翻訳 API（OpenAI 互換 Chat Completions）

翻訳は `OPENAI_BASE_URL` の OpenAI 互換 Chat Completions API（`POST {OPENAI_BASE_URL}/chat/completions`）を呼び出します。モデル ID は `OPENAI_MODEL` で必須設定します。認証は `Authorization: Bearer {OPENAI_API_KEY}` です。Structured Outputs は `response_format.type=json_schema`（`strict: true`）で指定し、用途別の JSON Schema で制約付きデコードします。system instruction へ schema を複製しません。プロバイダ固有のルーティング設定（`provider`）は送らず、エンドポイント選択はプロバイダ側の設定に任せます。

| 項目 | 値 |
|---|---|
| API | Chat Completions（非ストリーミング） |
| 試行タイムアウト | 60 秒（試行ごと） |
| 一時障害リトライ | ちょうど 1 回（1 秒待機後）。対象はタイムアウト・通信エラー・HTTP 429/5xx。契約違反・4xx（429以外）は再試行しない |
| temperature | 省略（プロバイダー既定値） |
| reasoning_effort | 未設定なら省略。`OPENAI_REASONING_EFFORT` を設定したときだけ送る |
| max_tokens | 4096（アプリケーション固定上限） |
| 出力形式 | 用途別の JSON Schema を `response_format.json_schema`（`strict: true`）で指定し、制約付きデコードする。翻訳用途のルートは当該リクエストの target languages をキーとするオブジェクト（配列ではない）。既存パーサーがキー集合・空文字・未知フィールドを厳密検証する。メッセージ翻訳の `attachment_descriptions` は翻訳対象の既存 alt があるときだけ required にし、`minItems`/`maxItems` でその件数に固定する。無いときはフィールド自体を出さない。余剰要素は適用しない。翻訳しなかった画像スロットはソースの Description を保持する |
| 画像入力 | 縮小JPEGを base64 data URL として Chat Completions の `image_url` パートで渡し、テキストパートの後ろ（明示 breakpoint があるときはその後）に置く。順序は現在メッセージの添付、履歴/リプライの `<image>`、OGP。現在メッセージの添付を確保した残枠でリプライ（新しい順）→履歴（新しい順）→OGP。添付+履歴/リプライ+OGP 合わせて最大 4 枚。リクエスト全体は 3.5MB 制約に収める |

**環境変数（必須）:** `OPENAI_BASE_URL`、`OPENAI_API_KEY`、`OPENAI_MODEL`。任意: `OPENAI_REASONING_EFFORT`（未設定で省略。`none` / `minimal` / `low` / `medium` / `high` / `xhigh` / `max`）、`TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN`（デフォルト `100000`）、`TRANSLATION_DEBUG_LOG_PATH`（未設定でデバッグログ無効）。

**呼び出し契約:**

- 全対象言語を1リクエストで生成する。分割・別プロバイダーへのfallbackは行わない。画像は縮小JPEGの data URL を `image_url` としてテキストパートの後ろ（明示 breakpoint があるときはその後）に置く（テキストのみなら user `content` は文字列）
- 試行ごとに60秒の期限を付け、タイムアウト・通信エラー・HTTP 429/5xx に限り1秒待機してちょうど1回だけ再試行する。親コンテキストのキャンセル、HTTP 4xx（429以外）、不正JSON・`finish_reason=length` 等の契約違反は再試行しない
- 用途別の schema を `response_format.json_schema`（`strict: true`）で送り、制約付きデコードする（通常メッセージ・投票・スレッド作成・話題要約）。翻訳用途のルートはリクエストの target languages をキーとするオブジェクトにし、English などの英語名は出せないようにする。キー集合・空文字・未知フィールドはパーサーで厳密検証する。メッセージ翻訳の `attachment_descriptions` は翻訳対象の既存 alt があるときだけ required にし、`minItems`/`maxItems` でその件数に固定する。無いときはフィールド自体を出さない。余剰要素は適用しない。翻訳しなかった画像スロットはソースの Description を保持する
- スレッド作成時はタイトル（`name`）と初期本文（`message`）を1リクエストで翻訳する。ソースに本文がない場合だけ `message` 空を許容する
- 話題要約は翻訳を待たせず、捨てた履歴枠だけを対象にする別リクエストである。出力は短い `summary` 文字列で、翻訳の `prompt_cache_key` は共有しない。トークンはギルドの翻訳レート制限に含める。失敗しても翻訳は継続する
- `finish_reason=length`（`max_tokens`到達）、不正JSON、言語欠落等は全体を fail-closed とし、部分的な翻訳を投稿しない
- Discord ID などの request metadata は送信しない。既定ではプロンプト・応答・認証情報・プロバイダーエラー本文をアプリログへ出さず、失敗時は安全なtype、code、param、request IDだけを記録する
- `TRANSLATION_DEBUG_LOG_PATH` を設定した場合だけ、障害調査および効果測定用に1往復1行のJSON Linesを指定ファイルへ追記する（リトライ時は最大2行）。合成した system / 安定ユーザー / 履歴ユーザー / 可変ユーザープロンプト、`prompt_cache_key`、プロバイダーが報告した `cached_tokens` に基づくキャッシュヒット、usage（input / output / cached / reasoning / 報告されたUSD料金）、クライアント実測の所要時間（全体・HTTP待ち・本文読み取り・試行番号・終了時刻）、同じ応答にある場合のみ `created` / `openai-processing-ms` / `Server-Timing` / usage の時間フィールド、リクエストペイロード、生のレスポンス本文、HTTPステータス、失敗理由、相関用のguild ID・message IDを記録する。認証情報は記録せず、プロバイダーへ送るフィールドも変わらない。料金・キャッシュヒット・プロバイダー側の生成時間は応答（およびそのHTTPヘッダ）に含まれているときだけ記録し、追加の generation API や単価表は使わない
- デプロイ時は5分期限で認証情報・モデルアクセス・レスポンス契約をprewarm検証し、成功後だけバイナリとenvを置換する

---

## 4. プロンプトインジェクション対策

翻訳プロンプトには以下の対策が施されています。

### プレースホルダー保護 (`placeholders.go`)

翻訳前に以下の要素を `[TYPE:label]` 形式のプレースホルダーに置き換え、翻訳後に復元します。同じ `TYPE:label` が複数回登場する場合のみ、2 回目以降に連番サフィックス（`:2`, `:3`, …）を付加します。

| 元の形式 | プレースホルダー |
|---|---|
| `<:name:id>`, `<a:name:id>` | `[EMOJI:name]` |
| `<@id>` (Alice) | `[USER:Alice]` |
| `<#id>` (#general) | `[CHANNEL:general]`（送信元チャンネル名） |
| `<@&id>` (@mod) | `[ROLE:mod]` |
| `</command:id>` | `[CMD:command]` |
| `<t:unix>` | `[TIME]` |
| `https://host/path` | `[SITE:N]`（本文中の URL 出現順の連番。title がある場合のみ `<site id="N" title="...">` を付与） |
| `` `code` ``, ` ```code``` ` | `[CODE]` |

名前が取得できないメンションは `[USER]`, `[CHANNEL]`, `[ROLE]` のようにラベルを省略します。

スポイラー (`||...||`) はマスクせず、マーカーを保持したまま内側のテキストを翻訳します。

### XML エスケープ

プロンプト内のすべてのユーザーコンテンツ（チャンネル名・サーバー説明・メッセージ本文など）は XML エスケープされます。

### システムプロンプト設計

- すべての `<discord_context>`、`<recent_context>`、`<topic_summary>`、`<reply_context>`、`<site_context>`、`<final_message>` 内コンテンツを「信頼できないDiscordのコンテンツ」として扱うよう指示
- 翻訳先言語の変更・コード出力・要約などの指示を無視するよう指示
- `[UPPERCASE:...]` プレースホルダは文字どおりコピーし、改変・翻訳・新規捏造をしない。ソース（`<final_message>` / `<poll>` / `<thread_create>`）にある明示テキストだけを訳し、リアクション追加・画像描写・前文脈の続き書きをしない
- メッセージ翻訳の system 末尾に、固定の日英 Few-shot（短文リアクション、プレースホルダ保持、ネイティブのチャット口語の見本）を置く。英語は見本の訳先であり、`<target_languages>` の各言語でも同じ自然さを使う。プレースホルダは見本どおり文字どおり残す。投票・スレッド作成・話題要約の system とユーザー XML には載せない
- メッセージ翻訳の system（不変領域）は推定 1200 トークン前後にサイズし、system ブレークポイントだけで典型プロバイダーのキャッシュ下限（1024）を超える。1500 を超えない。これで不変領域のキャッシュヒット率を上げ、コストを抑える

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
    source_image_attachments TEXT, -- 履歴・リプライ文脈用の画像添付 JSON
    PRIMARY KEY (source_message_id, source_channel_id, target_channel_id)
)

-- 返信元の逆引き
message_references (
    source_message_id INTEGER,
    source_channel_id TEXT,
    referenced_message_id INTEGER,
    referenced_channel_id TEXT,
    PRIMARY KEY (source_message_id, source_channel_id)
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

-- フォーラム/メディアタグのチャンネル間対応（無向。保存時に channel_a_id < channel_b_id へ正規化）
forum_tag_maps (
    guild_id TEXT,
    group_id TEXT,
    channel_a_id TEXT,
    tag_a_id TEXT,
    channel_b_id TEXT,
    tag_b_id TEXT,
    PRIMARY KEY (guild_id, group_id, channel_a_id, tag_a_id, channel_b_id),
    UNIQUE (guild_id, group_id, channel_b_id, tag_b_id, channel_a_id)
)

-- ピン留め状態（エコー防止用）
pin_states (
    channel_id TEXT,
    message_id INTEGER, -- Discord snowflake
    pinned INTEGER,
    PRIMARY KEY (channel_id, message_id)
)

-- メッセージ同期の冪等性キー（msglink:{sourceChannel}:{sourceMessage}:{targetChannel}）
processed_events (event_id, created_at INTEGER) -- Unix milliseconds
glossary_entries (guild_id, source_term, source_term_key, preferred_translation, attribute, always_include, created_by, created_at INTEGER) -- Unix milliseconds

-- Botがギルドから削除された時刻
guild_removals (
    guild_id TEXT PRIMARY KEY,
    removed_at INTEGER -- Unix milliseconds
)

-- 世代切替で捨てた会話の話題要約（次の翻訳の履歴文脈用）
topic_summaries (
    guild_id TEXT,
    location_key TEXT PRIMARY KEY,
    generation_id TEXT,
    summary TEXT,
    created_at INTEGER -- Unix milliseconds
)
```

### ギルド削除後のデータ保持

`GUILD_DATA_RETENTION_DAYS` は、Bot がギルドから削除された後にそのギルドの SQLite データを保持する日数です。未指定または `0` では自動削除しません。正の整数の場合、削除確認から指定日数を超えたギルドを起動時および 24 時間ごとに削除します。負数または整数以外は起動時エラーになります。

一時的な Gateway 障害を示す `GUILD_DELETE` の `unavailable=true` は削除として記録しません。保持期限前に利用可能な `GUILD_CREATE` を受け取った場合は削除予定を取り消します。削除処理は対象ギルドごとのトランザクションで、`translation_groups`、`group_channels`、`glossary_entries`、`source_allowlists`、`topic_summaries` と、そのギルドの登録チャンネルおよび同期済みスレッドに属する `message_links`、孤立した `message_references`、`thread_links`、`pin_states` を SQLite から削除します。Discord 上のメッセージや Webhook は削除しません。

---

## 6. イベント処理フロー

```
Discordゲートウェイ
        │
        ├── InteractionCreate (スラッシュコマンド)
        │       └── CommandHandler.Handle
        │               ├── /new-channel  → Store.CreateGroupWithChannel
        │               ├── /join-channel → Store.JoinChannel（フォーラム時はタグ UI）
        │               ├── /leave-channel → Store.LeaveChannel
        │               ├── /delete-group → Store.DeleteGroup
        │               ├── /edit-forum-tags → タグ対応付け UI
        │               ├── /set-style → Store.SetGroupStyle
        │               ├── MessageComponent (ftm:*) → forum tag map upsert/delete
        │               ├── /add-glossary → Store.UpsertGlossaryEntry
        │               ├── /list-glossary → Store.ListGlossaryEntries
        │               └── /remove-glossary → Store.RemoveGlossaryEntry
        │
        ├── MessageCreate
        │       ├── ボット/ウェブフック → スキップ
        │       ├── ThreadStarterMessage → ensureThreadSynced のみ
        │       ├── ensureThreadSynced（未リンクのスレッドだけ対向を作成）
        │       ├── handleThreadMessageCreate（スレッド内メッセージ。ミラー先も逆向きに同期）
        │       └── 通常メッセージ → Translate → SendWebhook → SaveMessageLink
        │
        ├── MessageUpdate → Translate → EditWebhook
        ├── MessageDelete → DeleteWebhook
        ├── MessageReactionAdd/Remove → SyncReaction
        ├── ThreadCreate → SyncThreadCreateFromGateway（applied_tags 含む）
        ├── ThreadUpdate → SyncThreadUpdate（名前および applied_tags）
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

## 8. ローカル実行

```sh
cp .env.example .env
# .env を編集して DISCORD_TOKEN と OpenAI 互換 API の認証情報を入力
go run ./cmd/discord-auto-translator
```
