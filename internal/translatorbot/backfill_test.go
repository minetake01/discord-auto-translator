package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Backfill requirements: the join-time job translates the existing history
// of the group into the new channel, holds live mirrors until it has caught
// up, and keeps every mirror editable and in sync like a live one.

type backfillFixture struct {
	store      *Store
	discord    *fakeDiscordAPI
	translator Translator
	service    *Service
	job        BackfillJob
}

// newBackfillFixture creates group g with source channel ja, then joins en
// with a backfill job. history is the source channel history.
func newBackfillFixture(t *testing.T, translator Translator, history ...DiscordMessage) *backfillFixture {
	t.Helper()
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{history: map[string][]DiscordMessage{"ja": history}}
	service := NewService(store, discord, translator)
	service.backfill.sleep = func(context.Context, time.Duration) error { return nil }
	stubImageHTTP(service)
	job, err := store.JoinChannelWithBackfill(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}, "u")
	if err != nil {
		t.Fatal(err)
	}
	return &backfillFixture{store: store, discord: discord, translator: translator, service: service, job: job}
}

func (f *backfillFixture) run(t *testing.T) {
	t.Helper()
	if err := f.service.runBackfillJob(context.Background(), f.job); err != nil {
		t.Fatal(err)
	}
}

func sourceMessage(id, content string) DiscordMessage {
	return DiscordMessage{ID: id, ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: content}
}

func postedLine(t *testing.T, sourceMessageID string) string {
	t.Helper()
	line, err := withTimestampLine(sourceMessageID, "")
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func sentContents(discord *fakeDiscordAPI) []string {
	var out []string
	for _, msg := range discord.webhookSends() {
		out = append(out, msg.Content)
	}
	return out
}

func TestBackfillPostsHistoryOldestFirstWithTimestampLines(t *testing.T) {
	translator := &echoTranslator{}
	f := newBackfillFixture(t, translator,
		sourceMessage("100000000000000003", "三"),
		sourceMessage("100000000000000001", "一"),
		sourceMessage("100000000000000002", "二"),
	)
	f.run(t)

	want := []string{
		postedLine(t, "100000000000000001") + "\n[en] 一",
		postedLine(t, "100000000000000002") + "\n[en] 二",
		postedLine(t, "100000000000000003") + "\n[en] 三",
	}
	got := sentContents(f.discord)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sent = %q, want %q", got, want)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("consecutive messages must share one request, got %d requests", len(translator.contexts))
	}
	links, err := f.store.MessageTargets(context.Background(), "ja", "100000000000000002")
	if err != nil || len(links) != 1 || !links[0].TimestampLine {
		t.Fatalf("links = %#v, err = %v", links, err)
	}
	if held, _ := f.store.BackfillTargetHeld(context.Background(), "g", "en"); held {
		t.Fatal("finished job must release the target")
	}
}

func TestBackfillLeavesOutMirrorsAndLinkedMessages(t *testing.T) {
	ctx := context.Background()
	f := newBackfillFixture(t, &echoTranslator{},
		sourceMessage("100000000000000001", "既存"),
		sourceMessage("100000000000000002", "ミラー"),
		sourceMessage("100000000000000003", "新規"),
	)
	// 1 is already linked to the target; 2 is a mirror the bot wrote.
	for _, link := range []MessageLink{
		{SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g", TargetChannelID: "en", TargetMessageID: "700000000000000001", TargetLanguage: "en"},
		{SourceMessageID: "100000000000000099", SourceChannelID: "other", GroupID: "g", TargetChannelID: "ja", TargetMessageID: "100000000000000002", TargetLanguage: "ja"},
	} {
		if err := f.store.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}
	f.run(t)

	got := sentContents(f.discord)
	if len(got) != 1 || !strings.HasSuffix(got[0], "[en] 新規") {
		t.Fatalf("sent = %q, want only the new message", got)
	}
}

func TestBackfillHoldsLiveMessagesAndPostsThemOnCatchUp(t *testing.T) {
	ctx := context.Background()
	older := sourceMessage("100000000000000001", "古い")
	live := sourceMessage("100000000000000002", "待機中")
	f := newBackfillFixture(t, &echoTranslator{}, older, live)

	if err := f.service.HandleMessageCreate(ctx, live); err != nil {
		t.Fatal(err)
	}
	if len(f.discord.webhookSends()) != 0 {
		t.Fatalf("live message must be held while the job exists: %q", sentContents(f.discord))
	}
	f.run(t)
	got := sentContents(f.discord)
	if len(got) != 2 || !strings.HasSuffix(got[1], "[en] 待機中") || !strings.HasPrefix(got[1], "-# <t:") {
		t.Fatalf("held message must be posted by the job with a timestamp line: %q", got)
	}

	after := sourceMessage("100000000000000003", "その後")
	if err := f.service.HandleMessageCreate(ctx, after); err != nil {
		t.Fatal(err)
	}
	got = sentContents(f.discord)
	if len(got) != 3 || got[2] != "[en] その後" {
		t.Fatalf("after the job live mirroring resumes without a timestamp line: %q", got)
	}
}

func TestBackfillCatchesUpMessagesThatArriveDuringTheRun(t *testing.T) {
	f := newBackfillFixture(t, &echoTranslator{}, sourceMessage("100000000000000001", "最初"))
	late := sourceMessage("100000000000000002", "途中")
	injected := false
	f.discord.sendErr = func(WebhookSend) error {
		if !injected {
			injected = true
			f.discord.history["ja"] = append(f.discord.history["ja"], late)
		}
		return nil
	}
	f.run(t)

	got := sentContents(f.discord)
	if len(got) != 2 || !strings.HasSuffix(got[1], "[en] 途中") {
		t.Fatalf("sent = %q", got)
	}
}

type contentFailTranslator struct {
	echoTranslator
	bad      string
	requests int
}

func (c *contentFailTranslator) TranslateBatch(ctx context.Context, prepared preparedTranslation) (BatchTranslationResult, error) {
	c.requests++
	for _, source := range prepared.batchSources {
		if strings.Contains(source, c.bad) {
			return BatchTranslationResult{}, fmt.Errorf("%w: unusable", errTranslationContent)
		}
	}
	return echoBatch(prepared), nil
}

func TestBackfillSplitsFailingBatchAndSkipsOnlyTheBrokenMessage(t *testing.T) {
	translator := &contentFailTranslator{bad: "BAD"}
	f := newBackfillFixture(t, translator,
		sourceMessage("100000000000000001", "a"),
		sourceMessage("100000000000000002", "b"),
		sourceMessage("100000000000000003", "BAD"),
		sourceMessage("100000000000000004", "d"),
	)
	f.run(t)

	got := sentContents(f.discord)
	if len(got) != 4 {
		t.Fatalf("sent = %q, want three mirrors and one gap notice", got)
	}
	prefix, _, _ := strings.Cut(localizedUIString("en", uiKeyBackfillSkipped), "%s")
	if !strings.HasPrefix(got[2], prefix) || !strings.Contains(got[2], "unusable") {
		t.Fatalf("the gap notice must sit where the broken message belongs: %q", got)
	}
	if !strings.Contains(got[0], "[en] a") || !strings.Contains(got[3], "[en] d") {
		t.Fatalf("messages around the gap must still be posted: %q", got)
	}
	for _, content := range []string{got[0], got[1], got[3]} {
		if strings.Contains(content, "BAD") {
			t.Fatalf("the broken message must not be posted: %q", got)
		}
	}
	if len(f.discord.channelMessages) != 0 {
		t.Fatalf("skipped messages must not notify the source channel: %#v", f.discord.channelMessages)
	}
}

func TestBackfillGapNoticeInsideAThreadUsesTheThreadID(t *testing.T) {
	const threadID = "100000000000000002"
	starter := sourceMessage(threadID, "スレッドの元")
	starter.StartsThread = true
	inThread := func(id, content string) DiscordMessage {
		m := sourceMessage(id, content)
		m.ChannelID, m.ParentChannelID, m.ThreadName = threadID, "ja", "話題"
		return m
	}
	f := newBackfillFixture(t, &contentFailTranslator{bad: "BAD"}, starter)
	f.discord.history[threadID] = []DiscordMessage{inThread("100000000000000005", "BAD")}
	f.discord.publicThreads = []DiscordThread{{ID: threadID, ParentID: "ja", Name: "話題", LastMessageID: "100000000000000005"}}
	f.run(t)

	sends := f.discord.webhookSends()
	if len(sends) == 0 {
		t.Fatal("no mirrors posted")
	}
	notice := sends[len(sends)-1]
	if notice.ThreadID == "" || !strings.Contains(notice.Content, "unusable") {
		t.Fatalf("gap notice must be posted inside the target thread: %#v", sends)
	}
}

func TestBackfillWaitsOutRateLimitInsteadOfSkipping(t *testing.T) {
	translator := &flakyBatchTranslator{failures: 2, err: errTranslationRateLimited}
	f := newBackfillFixture(t, translator, sourceMessage("100000000000000001", "a"))
	waits := 0
	f.service.backfill.sleep = func(context.Context, time.Duration) error { waits++; return nil }
	f.run(t)

	if len(f.discord.webhookSends()) != 1 || waits != 2 {
		t.Fatalf("sends = %d, waits = %d", len(f.discord.webhookSends()), waits)
	}
}

type flakyBatchTranslator struct {
	echoTranslator
	failures int
	err      error
}

func (f *flakyBatchTranslator) TranslateBatch(ctx context.Context, prepared preparedTranslation) (BatchTranslationResult, error) {
	if f.failures > 0 {
		f.failures--
		return BatchTranslationResult{}, f.err
	}
	return echoBatch(prepared), nil
}

func TestBackfillEditKeepsTimestampLineAndReplyQuote(t *testing.T) {
	ctx := context.Background()
	first := sourceMessage("100000000000000001", "元の発言")
	reply := sourceMessage("100000000000000002", "返信です")
	reply.ReferencedMessageID, reply.ReferencedMessageChannelID, reply.ReferencedMessageContent = first.ID, "ja", first.Content
	f := newBackfillFixture(t, &echoTranslator{}, first, reply)
	f.discord.messageContents = map[string]string{"ja\x00" + first.ID: first.Content}
	f.run(t)

	sent := sentContents(f.discord)
	if len(sent) != 2 || !strings.Contains(sent[1], "\n> ") {
		t.Fatalf("reply mirror must quote its target: %q", sent)
	}
	edited := reply
	edited.Content = "返信を直しました"
	edited.ReferencedMessageID, edited.ReferencedMessageChannelID, edited.ReferencedMessageContent = "", "", ""
	if err := f.service.HandleMessageUpdate(ctx, edited); err != nil {
		t.Fatal(err)
	}
	if len(f.discord.webhookEdits) != 1 {
		t.Fatalf("edits = %#v", f.discord.webhookEdits)
	}
	want := strings.Replace(sent[1], "[en] 返信です", "[en] 返信を直しました", 1)
	if got := f.discord.webhookEdits[0].content; got != want {
		t.Fatalf("edit = %q, want %q", got, want)
	}
}

func TestBackfillPostsParentChannelThenThreadsAndRestoresArchivedState(t *testing.T) {
	const threadID = "100000000000000002"
	starter := sourceMessage(threadID, "スレッドの元")
	starter.StartsThread = true
	inThread := func(id, content string) DiscordMessage {
		m := sourceMessage(id, content)
		m.ChannelID, m.ParentChannelID, m.ThreadName = threadID, "ja", "話題"
		return m
	}
	f := newBackfillFixture(t, &echoTranslator{},
		sourceMessage("100000000000000001", "前"),
		starter,
		sourceMessage("100000000000000009", "後"),
	)
	f.discord.history[threadID] = []DiscordMessage{inThread("100000000000000005", "スレ1"), inThread("100000000000000006", "スレ2")}
	f.discord.publicThreads = []DiscordThread{{ID: threadID, ParentID: "ja", Name: "話題", Archived: true, Locked: true, LastMessageID: "100000000000000006"}}
	f.run(t)

	sends := f.discord.webhookSends()
	if len(sends) != 5 {
		t.Fatalf("sent = %q", sentContents(f.discord))
	}
	for i := 0; i < 3; i++ {
		if sends[i].ThreadID != "" {
			t.Fatalf("parent channel messages come first: %#v", sends)
		}
	}
	targetThread := fakeThreadID(f.discord.nextID)
	for _, msg := range sends[3:] {
		if msg.ThreadID == "" || !strings.HasPrefix(msg.Content, "-# <t:") {
			t.Fatalf("thread messages follow in the created thread with timestamps: %#v", sends)
		}
	}
	if len(f.discord.threadStates) == 0 {
		t.Fatal("target thread must follow the archived source")
	}
	last := f.discord.threadStates[len(f.discord.threadStates)-1]
	if last.threadID != sends[3].ThreadID || !last.archived || !last.locked {
		t.Fatalf("last state change = %#v, target thread %s", last, targetThread)
	}
}

// A target channel must announce a thread where the source thread was
// created. Creating every thread at the end of the run bunches the channel's
// own announcements into one burst far away from the messages around them.
func TestBackfillCreatesThreadsWhereTheSourceCreatedThem(t *testing.T) {
	base := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	id := func(d time.Duration) string { return snowflakeForTime(base.Add(d), 0) }
	var (
		first        = id(0)
		earlyStarter = id(10 * time.Minute)
		earlyCreated = base.Add(15 * time.Minute)
		middle       = id(20 * time.Minute)
		lateStarter  = id(30 * time.Minute)
		lateCreated  = base.Add(40 * time.Minute)
	)
	starter := func(id, content string) DiscordMessage {
		m := sourceMessage(id, content)
		m.StartsThread = true
		return m
	}
	f := newBackfillFixture(t, &echoTranslator{},
		sourceMessage(first, "最初"),
		starter(earlyStarter, "早いスレッド"),
		sourceMessage(middle, "途中"),
		starter(lateStarter, "遅いスレッド"),
	)
	f.discord.publicThreads = []DiscordThread{
		{ID: lateStarter, ParentID: "ja", Name: "遅いスレッド", CreatedAt: lateCreated},
		{ID: earlyStarter, ParentID: "ja", Name: "早いスレッド", CreatedAt: earlyCreated},
	}
	f.run(t)

	want := []string{"message", "message", "thread", "message", "message", "thread"}
	if got := f.discord.eventLog(); !slices.Equal(got, want) {
		t.Fatalf("Discord calls = %v, want %v", got, want)
	}
	threads := f.discord.threads
	if len(threads) != 2 {
		t.Fatalf("threads = %#v", threads)
	}
	if threads[0].messageID != fakeSentID(2) || !strings.Contains(threads[0].name, "早い") {
		t.Fatalf("first thread = %#v, want the early thread on the mirror of its starter", threads[0])
	}
	if threads[1].messageID != fakeSentID(5) || !strings.Contains(threads[1].name, "遅い") {
		t.Fatalf("second thread = %#v, want the late thread on the mirror of its starter", threads[1])
	}
}

// A thread created at the same moment as a message may only be announced after
// it: the starter a thread started from a message hangs on has to be there
// first.
func TestBackfillCreatesAThreadAfterTheMessageItSharesATimeWith(t *testing.T) {
	base := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	id := func(d time.Duration) string { return snowflakeForTime(base.Add(d), 0) }
	starter := sourceMessage(id(0), "スレッド元")
	starter.StartsThread = true
	f := newBackfillFixture(t, &echoTranslator{}, starter, sourceMessage(id(10*time.Minute), "次"))
	f.discord.publicThreads = []DiscordThread{{ID: starter.ID, ParentID: "ja", Name: "スレッド元", CreatedAt: base}}
	f.run(t)

	if got, want := f.discord.eventLog(), []string{"message", "thread", "message"}; !slices.Equal(got, want) {
		t.Fatalf("Discord calls = %v, want %v", got, want)
	}
}

func TestBackfillAppliesReactionsAndPinToNewMirror(t *testing.T) {
	m := sourceMessage("100000000000000001", "ピン留め")
	m.Reactions = []string{"👍", "🎉"}
	m.Pinned = true
	f := newBackfillFixture(t, &echoTranslator{}, m)
	f.run(t)

	mirrorID := fakeSentID(1)
	if len(f.discord.reactions) != 2 || f.discord.reactions[0].channelID != "en" || f.discord.reactions[0].messageID != mirrorID {
		t.Fatalf("reactions = %#v", f.discord.reactions)
	}
	if len(f.discord.pinCalls) != 1 || !f.discord.pinCalls[0].pinned || f.discord.pinCalls[0].messageID != mirrorID {
		t.Fatalf("pins = %#v", f.discord.pinCalls)
	}
}

func TestBackfillIsCanceledWhenTheChannelLeaves(t *testing.T) {
	ctx := context.Background()
	f := newBackfillFixture(t, &echoTranslator{}, sourceMessage("100000000000000001", "a"))
	if err := f.store.LeaveChannel(ctx, "guild", "g", "en"); err != nil {
		t.Fatal(err)
	}
	if exists, _ := f.store.BackfillJobExists(ctx, f.job.ID); exists {
		t.Fatal("leaving must remove the job")
	}
	if err := f.service.runBackfillJob(ctx, f.job); !errors.Is(err, errBackfillCanceled) {
		t.Fatalf("err = %v", err)
	}
	if len(f.discord.webhookSends()) != 0 {
		t.Fatal("a canceled job must not post")
	}
}

func TestBackfillIsCanceledWhenTheGroupIsDeleted(t *testing.T) {
	ctx := context.Background()
	f := newBackfillFixture(t, &echoTranslator{}, sourceMessage("100000000000000001", "a"))
	if err := f.store.DeleteGroup(ctx, "guild", "g"); err != nil {
		t.Fatal(err)
	}
	if jobs, err := f.store.BackfillJobs(ctx); err != nil || len(jobs) != 0 {
		t.Fatalf("jobs = %#v, err = %v", jobs, err)
	}
}

func TestBackfillJobsOfAGuildRunOneAtATimeInOrder(t *testing.T) {
	ctx := context.Background()
	f := newBackfillFixture(t, &echoTranslator{})
	second, err := f.store.JoinChannelWithBackfill(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "fr", Language: "fr", WebhookID: "w-fr", WebhookToken: "t-fr",
	}, "u")
	if err != nil {
		t.Fatal(err)
	}
	next, ok, err := f.store.NextBackfillJob(ctx, "guild")
	if err != nil || !ok || next.ID != f.job.ID {
		t.Fatalf("next = %#v, %v, %v", next, ok, err)
	}
	if held, _ := f.store.BackfillTargetHeld(ctx, "g", "fr"); !held {
		t.Fatal("a queued job holds its target as well")
	}
	if err := f.store.FinishBackfillJob(ctx, f.job.ID); err != nil {
		t.Fatal(err)
	}
	next, _, _ = f.store.NextBackfillJob(ctx, "guild")
	if next.ID != second.ID {
		t.Fatalf("next = %#v", next)
	}
}

func TestBackfillEstimateCountsOnlyWhatWouldBePosted(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w", WebhookToken: "t",
	}); err != nil {
		t.Fatal(err)
	}
	withImage := sourceMessage("100000000000000002", "画像つき")
	withImage.Attachments = []DiscordAttachment{{URL: "https://cdn.discordapp.com/a.png", Filename: "a.png", ContentType: "image/png"}}
	bot := sourceMessage("100000000000000003", "bot")
	bot.Bot, bot.AuthorID = true, "123456789012345678" // not on the allowlist
	discord := &fakeDiscordAPI{history: map[string][]DiscordMessage{"ja": {
		sourceMessage("100000000000000001", "一"), withImage, bot,
	}}}
	service := NewService(store, discord, &echoTranslator{})

	est, err := service.EstimateBackfill(ctx, "guild", "g")
	if err != nil {
		t.Fatal(err)
	}
	if est.Messages != 2 || est.Images != 1 || est.InputTokens <= 0 || est.OutputTokens <= 0 || est.Duration <= 0 {
		t.Fatalf("estimate = %#v", est)
	}
}

// Archived and locked threads: the bot opens them for the operation and puts
// them back, and mirrors the source's state to the peer threads.

func threadClosedError(code int) error {
	return &discordgo.RESTError{Message: &discordgo.APIErrorMessage{Code: code}}
}

func seedThreadPair(t *testing.T, store *Store, discord *fakeDiscordAPI) {
	t.Helper()
	seedGroup(t, store)
	if err := store.SaveThreadLink(context.Background(), ThreadLink{
		GroupID: "g", SourceThreadID: "100000000000000005", SourceChannelID: "ja",
		TargetThreadID: "800000000000000001", TargetChannelID: "en", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}
	discord.registerThread("100000000000000005", "ja")
	discord.registerThread("800000000000000001", "en")
}

func TestSendToLockedThreadUnlocksAndRelocks(t *testing.T) {
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedThreadPair(t, store, discord)
	meta := discord.channels["800000000000000001"].ThreadMetadata
	meta.Locked = true
	discord.sendErr = func(WebhookSend) error {
		if meta.Locked {
			return threadClosedError(discordCodeThreadLocked)
		}
		return nil
	}

	err := service.HandleMessageCreate(context.Background(), DiscordMessage{
		ID: "100000000000000017", ChannelID: "100000000000000005", GuildID: "guild", ParentChannelID: "ja",
		AuthorID: "u", AuthorDisplayName: "u", Content: "ロック中",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookSends()) != 1 || !meta.Locked || meta.Archived {
		t.Fatalf("sends = %d, locked = %v, archived = %v", len(discord.webhookSends()), meta.Locked, meta.Archived)
	}
}

func TestEditInArchivedThreadReopensAndRestores(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedThreadPair(t, store, discord)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000017", SourceChannelID: "100000000000000005", GroupID: "g",
		TargetChannelID: "800000000000000001", TargetMessageID: "700000000000000001", TargetLanguage: "en",
		SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}
	meta := discord.channels["800000000000000001"].ThreadMetadata
	meta.Archived, meta.Locked = true, true
	discord.editErr = func(webhookEditCall) error {
		if meta.Archived {
			return threadClosedError(discordCodeThreadArchived)
		}
		return nil
	}

	err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000017", ChannelID: "100000000000000005", GuildID: "guild", ParentChannelID: "ja",
		AuthorID: "u", AuthorDisplayName: "u", Content: "after",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 1 || !meta.Archived || !meta.Locked {
		t.Fatalf("edits = %d, archived = %v, locked = %v", len(discord.webhookEdits), meta.Archived, meta.Locked)
	}
}

func TestThreadStateChangeSyncsToPeersWithoutEchoing(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedThreadPair(t, store, discord)
	const source, target = "100000000000000005", "800000000000000001"

	if err := service.SyncThreadState(ctx, source, true, false); err != nil {
		t.Fatal(err)
	}
	if !discord.channels[target].ThreadMetadata.Archived {
		t.Fatal("peer thread must follow the source")
	}
	calls := len(discord.threadStates)
	// The gateway reports the bot's own change back; it must not bounce.
	if err := service.SyncThreadState(ctx, target, true, false); err != nil {
		t.Fatal(err)
	}
	if len(discord.threadStates) != calls {
		t.Fatalf("echo was synced back: %#v", discord.threadStates)
	}
	// A real change on the target syncs to the source.
	if err := service.SyncThreadState(ctx, target, false, false); err != nil {
		t.Fatal(err)
	}
	if discord.channels[source].ThreadMetadata.Archived {
		t.Fatal("a user change on the peer must reach the source")
	}
}

func TestBackfillStopCancelsTheJobAndReleasesTheTarget(t *testing.T) {
	ctx := context.Background()
	f := newBackfillFixture(t, &echoTranslator{}, sourceMessage("100000000000000001", "a"))
	stopped, err := f.store.CancelBackfillJobs(ctx, "guild", "en")
	if err != nil || stopped != 1 {
		t.Fatalf("stopped = %d, err = %v", stopped, err)
	}
	if err := f.service.runBackfillJob(ctx, f.job); !errors.Is(err, errBackfillCanceled) {
		t.Fatalf("err = %v", err)
	}
	if len(f.discord.webhookSends()) != 0 {
		t.Fatalf("a stopped job must not post: %q", sentContents(f.discord))
	}
	if held, _ := f.store.BackfillTargetHeld(ctx, "g", "en"); held {
		t.Fatal("a stopped job must stop holding its target")
	}
	if stopped, err := f.store.CancelBackfillJobs(ctx, "guild", "en"); err != nil || stopped != 0 {
		t.Fatalf("second stop = %d, err = %v", stopped, err)
	}
}

func TestBackfillReleasesItsHoldLockAndMemberCacheWhenTheJobEnds(t *testing.T) {
	f := newBackfillFixture(t, &echoTranslator{}, sourceMessage("100000000000000001", "a"))
	f.run(t)

	if held := len(f.service.backfill.holds); held != 0 {
		t.Fatalf("finished job kept %d hold locks", held)
	}
	if f.discord.forgetMemberCacheCalls != 1 {
		t.Fatalf("member cache drops = %d, want 1 per job", f.discord.forgetMemberCacheCalls)
	}
}

func TestBackfillEstimateReportsALowerBoundWhenTheHistoryIsTooLarge(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w", WebhookToken: "t",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{history: map[string][]DiscordMessage{}}
	for i := 0; i <= backfillEstimateArchivedThreadLimit; i++ {
		discord.publicThreads = append(discord.publicThreads, DiscordThread{
			ID: fakeThreadID(i), ParentID: "ja", Name: "t", Archived: true,
		})
	}
	service := NewService(store, discord, &echoTranslator{})

	est, err := service.EstimateBackfill(ctx, "guild", "g")
	if err != nil {
		t.Fatal(err)
	}
	if !est.Truncated {
		t.Fatalf("estimate = %#v, want a truncated scan", est)
	}
	if got := estimateCount(est); got != fmt.Sprintf("%d+", est.Messages) {
		t.Fatalf("estimate count = %q", got)
	}
}
