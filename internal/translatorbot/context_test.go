package translatorbot

// SPEC 3.8

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func snowflakeForTime(t time.Time, increment uint64) string {
	return strconv.FormatUint((uint64(t.UnixMilli()-discordEpochMillis)<<22)|increment, 10)
}
func historyLink(t time.Time, increment uint64, author, content string) MessageLink {
	return MessageLink{
		SourceMessageID:         snowflakeForTime(t, increment),
		SourceChannelID:         "ja",
		SourceAuthorDisplayName: author,
		SourceContentSnapshot:   content,
	}
}

// SPEC 3.8
func TestHandleMessageCreatePassesGuildDescriptionAndChannelTopic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		guildNames:        map[string]string{"guild": "Ship Room"},
		guildDescriptions: map[string]string{"guild": "Release coordination server"},
		channelNames:      map[string]string{"ja": "announcements-ja"},
		channelTopics:     map[string]string{"ja": "Japanese announcements"},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "出荷しました",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if got := translator.contexts[0]; got.GuildID != "guild" || got.MessageID != "100000000000000001" || got.ServerName != "Ship Room" || got.ServerDescription != "Release coordination server" || got.ChannelName != "announcements-ja" || got.ChannelTopic != "Japanese announcements" {
		t.Fatalf("unexpected translation context: %#v", got)
	}
}

// SPEC 3.8
func TestHandleMessageCreatePassesGroupStyleInstructions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SetGroupStyle(ctx, "guild", "g", "gaming", ""); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "GG",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	want := ResolveStyleInstructions("gaming", "")
	if translator.contexts[0].StyleInstructions != want {
		t.Fatalf("style instructions = %q, want %q", translator.contexts[0].StyleInstructions, want)
	}
}

// SPEC 3.8
func TestHandleMessageCreateIncludesSiteMetaInTranslationContext(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head>
			<meta property="og:title" content="Example Article">
			<meta property="og:description" content="About the article">
			<link rel="alternate" hreflang="en" href="https://example.com/en">
		</head></html>`)
	}))
	t.Cleanup(page.Close)
	service.urlPages.client = page.Client()
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Content: "これ見て " + page.URL,
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	tc := translator.contexts[0]
	if got := tc.SiteTitles[page.URL]; got != "Example Article" {
		t.Fatalf("SiteTitles[%q] = %q", page.URL, got)
	}
	if got := tc.SiteDescriptions[page.URL]; got != "About the article" {
		t.Fatalf("SiteDescriptions[%q] = %q", page.URL, got)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "[en] これ見て https://example.com/en" {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

// SPEC 3.8
func TestHandleMessageCreatePassesRecentHistory(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en",
		SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "前の発言",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "101", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "続きです",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	got := translator.contexts[0].History
	if len(got) != 1 || got[0].Author != "Alice" || got[0].Content != "前の発言" {
		t.Fatalf("unexpected history: %#v", got)
	}
}

// SPEC 3.8
func TestHandleMessageCreateExcludesHistoryOlderThan24Hours(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	now := time.Now().UTC()
	for _, link := range []MessageLink{
		{
			SourceMessageID: snowflakeForTime(now.Add(-25*time.Hour), 1), SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "old-target", TargetLanguage: "en",
			SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "昨日の発言",
		},
		{
			SourceMessageID: snowflakeForTime(now.Add(-23*time.Hour), 2), SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "recent-target", TargetLanguage: "en",
			SourceAuthorID: "bob-id", SourceAuthorDisplayName: "Bob", SourceContentSnapshot: "今日の発言",
		},
	} {
		if err := store.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: snowflakeForTime(now, 3), ChannelID: "ja", GuildID: "guild", AuthorID: "carol",
		AuthorDisplayName: "carol", Content: "続きです",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	got := translator.contexts[0].History
	if len(got) != 1 || got[0].Author != "Bob" || got[0].Content != "今日の発言" {
		t.Fatalf("unexpected history: %#v", got)
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesCombinesShortMessages(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	got := mergeConsecutiveMessages([]MessageLink{
		historyLink(now.Add(-3*time.Minute), 1, "Alice", "こんにちは"),
		historyLink(now.Add(-2*time.Minute), 2, "Alice", "元気？"),
	}, cutoff, nil)
	if len(got) != 1 {
		t.Fatalf("got %d slots, want 1: %#v", len(got), got)
	}
	if got[0].Author != "Alice" || got[0].Content != "こんにちは\n元気？" {
		t.Fatalf("unexpected merged message: %#v", got[0])
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesSkipsLongMessage(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	longContent := strings.Repeat("あ", mergeShortMessageMaxRunes+1)
	got := mergeConsecutiveMessages([]MessageLink{
		historyLink(now.Add(-3*time.Minute), 1, "Alice", "短い"),
		historyLink(now.Add(-2*time.Minute), 2, "Alice", longContent),
	}, cutoff, nil)
	if len(got) != 2 {
		t.Fatalf("got %d slots, want 2: %#v", len(got), got)
	}
	if got[0].Content != "短い" || got[1].Content != longContent {
		t.Fatalf("unexpected messages: %#v", got)
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesStopsAtCombinedLength(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	first := strings.Repeat("a", 100)
	second := strings.Repeat("b", 49)
	third := "c"
	got := mergeConsecutiveMessages([]MessageLink{
		historyLink(now.Add(-4*time.Minute), 1, "Alice", first),
		historyLink(now.Add(-3*time.Minute), 2, "Alice", second),
		historyLink(now.Add(-2*time.Minute), 3, "Alice", third),
	}, cutoff, nil)
	if len(got) != 2 {
		t.Fatalf("got %d slots, want 2: %#v", len(got), got)
	}
	if got[0].Content != first+"\n"+second {
		t.Fatalf("unexpected first slot: %q", got[0].Content)
	}
	if got[1].Content != third {
		t.Fatalf("unexpected second slot: %q", got[1].Content)
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesStopsAtCountLimit(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	links := make([]MessageLink, 0, mergeMaxCount+1)
	for i := 0; i < mergeMaxCount+1; i++ {
		links = append(links, historyLink(now.Add(time.Duration(-mergeMaxCount+i)*time.Minute), uint64(i+1), "Alice", "msg"))
	}
	got := mergeConsecutiveMessages(links, cutoff, nil)
	if len(got) != 2 {
		t.Fatalf("got %d slots, want 2: %#v", len(got), got)
	}
	wantMerged := strings.Repeat("msg\n", mergeMaxCount-1) + "msg"
	if got[0].Content != wantMerged {
		t.Fatalf("unexpected merged slot: %q", got[0].Content)
	}
	if got[1].Content != "msg" {
		t.Fatalf("unexpected overflow slot: %q", got[1].Content)
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesRespectsTimeWindow(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	got := mergeConsecutiveMessages([]MessageLink{
		historyLink(now.Add(-10*time.Minute), 1, "Alice", "最初"),
		historyLink(now.Add(-3*time.Minute), 2, "Alice", "あと"),
	}, cutoff, nil)
	if len(got) != 2 {
		t.Fatalf("got %d slots, want 2: %#v", len(got), got)
	}
	if got[0].Content != "最初" || got[1].Content != "あと" {
		t.Fatalf("unexpected messages: %#v", got)
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesStartsNewSlotForDifferentAuthor(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	got := mergeConsecutiveMessages([]MessageLink{
		historyLink(now.Add(-3*time.Minute), 1, "Alice", "A"),
		historyLink(now.Add(-2*time.Minute), 2, "Bob", "B"),
	}, cutoff, nil)
	if len(got) != 2 || got[0].Author != "Alice" || got[1].Author != "Bob" {
		t.Fatalf("unexpected authors: %#v", got)
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesLimitsHistorySlots(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	links := make([]MessageLink, 0, 5)
	for i := 0; i < 5; i++ {
		links = append(links, historyLink(now.Add(time.Duration(-5+i)*time.Minute), uint64(i+1), fmt.Sprintf("user-%d", i), "msg"))
	}
	got := mergeConsecutiveMessages(links, cutoff, nil)
	if len(got) != translationHistoryLimit {
		t.Fatalf("got %d slots, want %d: %#v", len(got), translationHistoryLimit, got)
	}
	if got[0].Author != "user-2" || got[2].Author != "user-4" {
		t.Fatalf("unexpected limited history: %#v", got)
	}
}

// SPEC 3.8
func TestMergeConsecutiveMessagesExcludesReplyKeysAndOldMessages(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-translationHistoryMaxAge)
	old := historyLink(now.Add(-25*time.Hour), 1, "Alice", "古い")
	reply := historyLink(now.Add(-3*time.Minute), 2, "Bob", "返信")
	recent := historyLink(now.Add(-2*time.Minute), 3, "Carol", "最近")
	got := mergeConsecutiveMessages([]MessageLink{old, reply, recent}, cutoff, map[string]bool{
		messageRefKey(reply.SourceChannelID, reply.SourceMessageID): true,
	})
	if len(got) != 1 || got[0].Author != "Carol" || got[0].Content != "最近" {
		t.Fatalf("unexpected filtered history: %#v", got)
	}
}

// SPEC 3.8
func TestHandleMessageDeleteExcludesDeletedMessageFromTranslationContext(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en",
		SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "前の発言",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageDelete(ctx, "guild", "ja", "100"); err != nil {
		t.Fatal(err)
	}
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "101", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "続きです",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if len(translator.contexts[0].History) != 0 {
		t.Fatalf("deleted message still in history: %#v", translator.contexts[0].History)
	}
}

// SPEC 3.8
func TestHandleMessageCreateIncludesCrossChannelOriginalHistory(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100", SourceChannelID: "en", GroupID: "g",
		TargetChannelID: "ja", TargetMessageID: "200", TargetLanguage: "ja",
		SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "Hello from English",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "101", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "続きです",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	got := translator.contexts[0].History
	if len(got) != 1 || got[0].Author != "Alice" || got[0].Content != "Hello from English" {
		t.Fatalf("unexpected history: %#v", got)
	}
}

// SPEC 3.8
func TestHandleMessageCreateReplyChainIncludesOriginalSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		messages: map[string]DiscordFetchedMessage{
			"ja\x00orig": {Content: "こんにちは", AuthorDisplayName: "Alice"},
		},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "こんにちは",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "こんにちは",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	got := translator.contexts[0].ReplyChain
	if len(got) != 1 || got[0].Author != "Alice" || got[0].Content != "こんにちは" {
		t.Fatalf("unexpected reply chain: %#v", got)
	}
}

// SPEC 3.8
func TestHandleMessageCreateReplyChainWalksUpToThreeMessages(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		messages: map[string]DiscordFetchedMessage{
			"ja\x00one":                {Content: "100000000000000021", AuthorDisplayName: "A"},
			"ja\x00two":                {Content: "second", AuthorDisplayName: "B", ReferencedChannelID: "ja", ReferencedMessageID: "100000000000000012"},
			"ja\x00100000000000000022": {Content: "third", AuthorDisplayName: "C", ReferencedChannelID: "ja", ReferencedMessageID: "100000000000000019"},
			"ja\x00100000000000000023": {Content: "fourth", AuthorDisplayName: "D", ReferencedChannelID: "ja", ReferencedMessageID: "100000000000000022"},
		},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	for _, link := range []MessageLink{
		{SourceMessageID: "100000000000000012", SourceChannelID: "ja", GroupID: "g", TargetChannelID: "en", TargetMessageID: "t1", TargetLanguage: "en", SourceAuthorDisplayName: "A", SourceContentSnapshot: "first"},
		{SourceMessageID: "100000000000000019", SourceChannelID: "ja", GroupID: "g", TargetChannelID: "en", TargetMessageID: "t2", TargetLanguage: "en", SourceAuthorDisplayName: "B", SourceContentSnapshot: "second"},
		{SourceMessageID: "100000000000000022", SourceChannelID: "ja", GroupID: "g", TargetChannelID: "en", TargetMessageID: "t3", TargetLanguage: "en", SourceAuthorDisplayName: "C", SourceContentSnapshot: "third"},
		{SourceMessageID: "100000000000000023", SourceChannelID: "ja", GroupID: "g", TargetChannelID: "en", TargetMessageID: "t4", TargetLanguage: "en", SourceAuthorDisplayName: "D", SourceContentSnapshot: "fourth"},
	} {
		if err := store.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "返信",
		ReferencedMessageID: "100000000000000023", ReferencedMessageChannelID: "ja",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := translator.contexts[0].ReplyChain
	if len(got) != 3 {
		t.Fatalf("unexpected reply chain length: %#v", got)
	}
	want := []ChatContextMessage{
		{Author: "B", Content: "second"},
		{Author: "C", Content: "third"},
		{Author: "D", Content: "fourth"},
	}
	for i, entry := range want {
		if got[i] != entry {
			t.Fatalf("reply chain[%d] = %#v, want %#v", i, got[i], entry)
		}
	}
}

// SPEC 3.8
func TestHandleMessageCreateReplyChainDedupesRecentHistory(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		messages: map[string]DiscordFetchedMessage{
			"ja\x00orig": {Content: "前の発言", AuthorDisplayName: "Alice"},
		},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en",
		SourceAuthorDisplayName: "Carol", SourceContentSnapshot: "別の発言",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "前の発言",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "101", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "返信",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctxData := translator.contexts[0]
	if len(ctxData.ReplyChain) != 1 || ctxData.ReplyChain[0].Content != "前の発言" {
		t.Fatalf("unexpected reply chain: %#v", ctxData.ReplyChain)
	}
	if len(ctxData.History) != 1 || ctxData.History[0].Content != "別の発言" {
		t.Fatalf("unexpected history: %#v", ctxData.History)
	}
}

// SPEC 3.8
func TestHandleMessageCreateReplyChainUsesOriginalWhenReplyingToMirror(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		messages: map[string]DiscordFetchedMessage{
			"en\x00translated": {Content: "[en] Hello", AuthorDisplayName: "Alice"},
		},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "en", GroupID: "g",
		TargetChannelID: "ja", TargetMessageID: "100000000000000018", TargetLanguage: "ja",
		SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "Hello",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "返信",
		ReferencedMessageID: "100000000000000018", ReferencedMessageChannelID: "ja",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := translator.contexts[0].ReplyChain
	if len(got) != 1 || got[0].Content != "Hello" {
		t.Fatalf("unexpected reply chain: %#v", got)
	}
}
