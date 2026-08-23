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
func TestHandleMessageCreateTagsLoadedOGPImagesAndDoesNotApplyTheirAlts(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/hero.png") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png1x1)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head>
			<meta property="og:title" content="Example Article">
			<meta property="og:description" content="About the article">
			<meta property="og:image" content="/hero.png">
		</head></html>`)
	}))
	t.Cleanup(page.Close)
	service.urlPages.client = page.Client()
	service.httpClient = page.Client()
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
	sites := translator.contexts[0].Sites
	if len(sites) != 1 || !sites[0].HasVisionImage {
		t.Fatalf("loaded OGP image should be marked on site context: %#v", sites)
	}
	if len(translator.visions) != 1 || len(translator.visions[0]) != 1 {
		t.Fatalf("OGP vision images = %#v", translator.visions)
	}
	if len(translator.userPrompts) != 1 || !strings.Contains(translator.userPrompts[0], `<site id="1" title="Example Article">About the article<image></image></site>`) {
		t.Fatalf("OGP image missing from prompt:\n%s", translator.userPrompts)
	}
	if len(discord.sent) != 1 || len(discord.sent[0].Files) != 0 {
		t.Fatalf("OGP images must not be reuploaded: %#v", discord.sent)
	}
}

// SPEC 3.8
func TestHandleMessageCreateReusesPromptCacheGenerationAcrossBurst(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	translator := &echoTranslator{}
	service := NewService(store, &fakeDiscordAPI{}, translator)
	seedGroup(t, store)

	firstID := "100000000000000001"
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: firstID, ChannelID: "ja", GuildID: "guild", AuthorID: "alice",
		AuthorDisplayName: "alice", Content: "はじめ",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "en", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "続き",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 2 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	first := translator.contexts[0]
	follow := translator.contexts[1]
	wantGeneration := "ja" + firstID
	if first.PromptCacheGeneration != wantGeneration {
		t.Fatalf("burst start generation = %q, want %q", first.PromptCacheGeneration, wantGeneration)
	}
	if follow.PromptCacheGeneration != wantGeneration {
		t.Fatalf("follow-up generation = %q, want %q", follow.PromptCacheGeneration, wantGeneration)
	}
	if first.PromptCacheLocation != "guild:g:group" || follow.PromptCacheLocation != first.PromptCacheLocation {
		t.Fatalf("location first=%q follow=%q", first.PromptCacheLocation, follow.PromptCacheLocation)
	}
}

// SPEC 3.8
func TestHandleMessageCreateStartsNewPromptCacheGenerationAfterIdleGap(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	translator := &echoTranslator{}
	service := NewService(store, &fakeDiscordAPI{}, translator)
	seedGroup(t, store)
	now := time.Now().UTC()
	old := snowflakeForTime(now.Add(-21*time.Minute), 1)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: old, SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "old-target", TargetLanguage: "en",
		SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "前のバースト",
	}); err != nil {
		t.Fatal(err)
	}
	current := snowflakeForTime(now, 3)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: current, ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "新しい話",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	got := translator.contexts[0]
	if len(got.History) != 0 {
		t.Fatalf("idle gap should drop history: %#v", got.History)
	}
	want := "ja" + current
	if got.PromptCacheGeneration != want {
		t.Fatalf("generation = %q, want current message %q (not empty or previous burst)", got.PromptCacheGeneration, want)
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
func TestHandleMessageCreateExcludesHistoryAcrossIdleGap(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	now := time.Now().UTC()
	for _, link := range []MessageLink{
		{
			SourceMessageID: snowflakeForTime(now.Add(-21*time.Minute), 1), SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "old-target", TargetLanguage: "en",
			SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "前のバースト",
		},
		{
			SourceMessageID: snowflakeForTime(now.Add(-4*time.Minute), 2), SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "recent-target", TargetLanguage: "en",
			SourceAuthorID: "bob-id", SourceAuthorDisplayName: "Bob", SourceContentSnapshot: "今のバースト",
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
	if len(got) != 1 || got[0].Author != "Bob" || got[0].Content != "今のバースト" {
		t.Fatalf("unexpected history: %#v", got)
	}
}

// SPEC 3.8
func TestSelectRecentHistoryDropsIdleBeforeCurrent(t *testing.T) {
	now := time.Now().UTC()
	current := snowflakeForTime(now, 2)
	got, _, _ := selectRecentHistory([]MessageLink{
		historyLink(now.Add(-16*time.Minute), 1, "Alice", "沈黙前"),
	}, current, nil)
	if len(got) != 0 {
		t.Fatalf("history after 16m silence = %#v", got)
	}
	got, _, _ = selectRecentHistory([]MessageLink{
		historyLink(now.Add(-14*time.Minute), 1, "Alice", "同じバースト"),
	}, current, nil)
	if len(got) != 1 || got[0].Content != "同じバースト" {
		t.Fatalf("history within 15m = %#v", got)
	}
}

func mergedHistory(links []MessageLink) []ChatContextMessage {
	slots := mergeConsecutiveMessages(links)
	out := make([]ChatContextMessage, len(slots))
	for i, slot := range slots {
		out[i] = slot.toMessage()
	}
	return out
}

// SPEC 3.8
func TestMergeConsecutiveMessagesCombinesShortMessages(t *testing.T) {
	now := time.Now().UTC()
	got := mergedHistory([]MessageLink{
		historyLink(now.Add(-3*time.Minute), 1, "Alice", "こんにちは"),
		historyLink(now.Add(-2*time.Minute), 2, "Alice", "元気？"),
	})
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
	longContent := strings.Repeat("あ", mergeShortMessageMaxRunes+1)
	got := mergedHistory([]MessageLink{
		historyLink(now.Add(-3*time.Minute), 1, "Alice", "短い"),
		historyLink(now.Add(-2*time.Minute), 2, "Alice", longContent),
	})
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
	first := strings.Repeat("a", 100)
	second := strings.Repeat("b", 49)
	third := "c"
	got := mergedHistory([]MessageLink{
		historyLink(now.Add(-4*time.Minute), 1, "Alice", first),
		historyLink(now.Add(-3*time.Minute), 2, "Alice", second),
		historyLink(now.Add(-2*time.Minute), 3, "Alice", third),
	})
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
	links := make([]MessageLink, 0, mergeMaxCount+1)
	for i := 0; i < mergeMaxCount+1; i++ {
		links = append(links, historyLink(now.Add(time.Duration(-mergeMaxCount+i)*time.Minute), uint64(i+1), "Alice", "msg"))
	}
	got := mergedHistory(links)
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
	got := mergedHistory([]MessageLink{
		historyLink(now.Add(-10*time.Minute), 1, "Alice", "最初"),
		historyLink(now.Add(-3*time.Minute), 2, "Alice", "あと"),
	})
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
	got := mergedHistory([]MessageLink{
		historyLink(now.Add(-3*time.Minute), 1, "Alice", "A"),
		historyLink(now.Add(-2*time.Minute), 2, "Bob", "B"),
	})
	if len(got) != 2 || got[0].Author != "Alice" || got[1].Author != "Bob" {
		t.Fatalf("unexpected authors: %#v", got)
	}
}

func historySlotTimes(history []ChatContextMessage) (oldest, newest time.Time) {
	for i, msg := range history {
		ts, ok := discordSnowflakeTime(msg.SourceMessageID)
		if !ok {
			continue
		}
		if i == 0 || ts.Before(oldest) {
			oldest = ts
		}
		if i == 0 || ts.After(newest) {
			newest = ts
		}
	}
	return oldest, newest
}

// SPEC 3.8
func TestSelectRecentHistoryCompressesCountOverflow(t *testing.T) {
	now := time.Now().UTC()
	links := make([]MessageLink, 0, 17)
	for i := 0; i < 17; i++ {
		links = append(links, historyLink(now.Add(time.Duration(i)*30*time.Second), uint64(i+1), fmt.Sprintf("user-%d", i), "msg"))
	}
	current := snowflakeForTime(now.Add(17*30*time.Second), 99)
	history, _, discarded := selectRecentHistory(links, current, nil)
	if len(history) == 0 || len(history) > 8 {
		t.Fatalf("got %d slots after count overflow, want 1..8: %#v", len(history), history)
	}
	if len(discarded) == 0 {
		t.Fatalf("count overflow should discard older slots, history=%#v", history)
	}
	if discarded[len(discarded)-1].SourceMessageID == history[0].SourceMessageID {
		t.Fatalf("discarded tail should not overlap kept history: discarded=%#v history=%#v", discarded, history)
	}
	oldest, newest := historySlotTimes(history)
	if oldest.IsZero() || newest.Sub(oldest) > 15*time.Minute {
		t.Fatalf("compressed span %s exceeds 15m: %#v", newest.Sub(oldest), history)
	}
}

// SPEC 3.8
func TestSelectRecentHistoryCompressesSpanOverflow(t *testing.T) {
	now := time.Now().UTC()
	links := make([]MessageLink, 0, 12)
	for i := 0; i < 12; i++ {
		links = append(links, historyLink(now.Add(time.Duration(i)*3*time.Minute), uint64(i+1), fmt.Sprintf("user-%d", i), "msg"))
	}
	current := snowflakeForTime(now.Add(12*3*time.Minute), 99)
	history, _, _ := selectRecentHistory(links, current, nil)
	if len(history) == 0 || len(history) > 8 {
		t.Fatalf("got %d slots after span overflow, want 1..8: %#v", len(history), history)
	}
	oldest, newest := historySlotTimes(history)
	if oldest.IsZero() || newest.Sub(oldest) > 15*time.Minute {
		t.Fatalf("compressed span %s exceeds 15m: %#v", newest.Sub(oldest), history)
	}
}

// SPEC 3.8
func TestSelectRecentHistoryKeepsFrozenMergedContentStable(t *testing.T) {
	now := time.Now().UTC()
	alice1 := historyLink(now.Add(-6*time.Minute), 1, "Alice", "こんにちは")
	alice2 := historyLink(now.Add(-5*time.Minute), 2, "Alice", "元気？")
	bob := historyLink(now.Add(-3*time.Minute), 3, "Bob", "別の人")
	carol := historyLink(now.Add(-1*time.Minute), 4, "Carol", "さらに")
	current := snowflakeForTime(now, 99)

	first, frozenCount, _ := selectRecentHistory([]MessageLink{alice1, alice2, bob}, current, nil)
	if len(first) != 2 || frozenCount != 1 || first[0].Content != "こんにちは\n元気？" {
		t.Fatalf("first history = %#v frozen=%d", first, frozenCount)
	}
	second, frozenCount, _ := selectRecentHistory([]MessageLink{alice1, alice2, bob, carol}, current, nil)
	if frozenCount != 2 || second[0].Content != first[0].Content || second[0].Author != first[0].Author {
		t.Fatalf("frozen slot changed: first=%#v second=%#v", first, second)
	}
}

// SPEC 3.8
func TestSelectRecentHistoryKeepsFrozenReplyOverlap(t *testing.T) {
	now := time.Now().UTC()
	reply := historyLink(now.Add(-4*time.Minute), 1, "Alice", "返信先")
	later := historyLink(now.Add(-2*time.Minute), 2, "Bob", "その後")
	current := snowflakeForTime(now, 99)
	replyKeys := map[string]bool{messageRefKey(reply.SourceChannelID, reply.SourceMessageID): true}

	history, frozenCount, _ := selectRecentHistory([]MessageLink{reply, later}, current, replyKeys)
	if frozenCount != 1 || len(history) != 2 || history[0].Content != "返信先" || history[1].Content != "その後" {
		t.Fatalf("frozen reply should remain: %#v frozen=%d", history, frozenCount)
	}

	other := historyLink(now.Add(-4*time.Minute), 3, "Bob", "その後")
	replyTail := historyLink(now.Add(-2*time.Minute), 4, "Alice", "返信先")
	tailKeys := map[string]bool{messageRefKey(replyTail.SourceChannelID, replyTail.SourceMessageID): true}
	tailOnly, frozenCount, _ := selectRecentHistory([]MessageLink{other, replyTail}, current, tailKeys)
	if frozenCount != 1 || len(tailOnly) != 1 || tailOnly[0].Content != "その後" {
		t.Fatalf("tail reply should drop: %#v frozen=%d", tailOnly, frozenCount)
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
		if got[i].Author != entry.Author || got[i].Content != entry.Content {
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

// SPEC 3.8
func TestSelectRecentHistoryKeepsImageOnlyMessages(t *testing.T) {
	now := time.Now().UTC()
	image := []DiscordAttachment{{URL: "https://cdn.discordapp.com/photo.png", Filename: "photo.png", ContentType: "image/png"}}
	history, _, _ := selectRecentHistory([]MessageLink{
		historyImageLink(now.Add(-2*time.Minute), 1, "Alice", "", image),
		historyLink(now.Add(-1*time.Minute), 2, "Bob", "なにこれ"),
	}, snowflakeForTime(now, 3), nil)
	if len(history) != 2 {
		t.Fatalf("image-only history dropped: %#v", history)
	}
	if history[0].Author != "Alice" || history[0].Content != "" || len(history[0].Attachments) != 1 {
		t.Fatalf("unexpected image-only slot: %#v", history[0])
	}
	if history[1].Content != "なにこれ" {
		t.Fatalf("unexpected follow-up: %#v", history[1])
	}

	empty, _, _ := selectRecentHistory([]MessageLink{
		historyLink(now.Add(-1*time.Minute), 1, "Alice", ""),
	}, snowflakeForTime(now, 2), nil)
	if len(empty) != 0 {
		t.Fatalf("empty text without images should still be dropped: %#v", empty)
	}
}

func historyImageLink(t time.Time, increment uint64, author, content string, images []DiscordAttachment) MessageLink {
	link := historyLink(t, increment, author, content)
	link.SourceImageAttachments = images
	return link
}

// SPEC 3.8
func TestHandleMessageCreateIncludesHistoryImagesInTranslationContext(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	stubImageHTTP(service)
	seedGroup(t, store)

	image := DiscordAttachment{URL: "https://cdn.discordapp.com/photo.png", Filename: "photo.png", ContentType: "image/png"}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "alice",
		AuthorDisplayName: "Alice", Attachments: []DiscordAttachment{image},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "Bob", Content: "これ何？",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	history := translator.contexts[0].History
	if len(history) != 1 || history[0].Author != "Alice" || history[0].Content != "" {
		t.Fatalf("unexpected history: %#v", history)
	}
	if len(history[0].Images) != 1 || history[0].Images[0].Index != 1 || history[0].Images[0].Filename != "photo.png" {
		t.Fatalf("history images = %#v", history[0].Images)
	}
	if len(translator.visions) != 1 || len(translator.visions[0]) != 1 {
		t.Fatalf("follow-up vision images = %#v", translator.visions)
	}
}

// SPEC 3.8
func TestHandleMessageCreateIncludesReplyImagesInTranslationContext(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	stubImageHTTP(service)
	seedGroup(t, store)
	image := DiscordAttachment{URL: "https://cdn.discordapp.com/sign.png", Filename: "sign.png", ContentType: "image/png", Description: "出口"}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorDisplayName: "Alice", SourceImageAttachments: []DiscordAttachment{image},
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "Bob", Content: "これ何？",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	reply := translator.contexts[0].ReplyChain
	if len(reply) != 1 || reply[0].Author != "Alice" {
		t.Fatalf("unexpected reply chain: %#v", reply)
	}
	if len(reply[0].Images) != 1 || reply[0].Images[0].Filename != "sign.png" || reply[0].Images[0].Description != "出口" {
		t.Fatalf("reply images = %#v", reply[0].Images)
	}
	if len(translator.visions[0]) != 1 {
		t.Fatalf("reply vision images = %#v", translator.visions)
	}
}

// SPEC 3.8
func TestHandleMessageCreateReservesVisionSlotsForCurrentAttachments(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	stubImageHTTP(service)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "old", TargetLanguage: "en",
		SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "見て",
		SourceImageAttachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/old.png", Filename: "old.png", ContentType: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}
	current := make([]DiscordAttachment, visionMaxImages)
	for i := range current {
		current[i] = DiscordAttachment{URL: fmt.Sprintf("https://cdn.discordapp.com/%d.png", i), Filename: fmt.Sprintf("%d.png", i), ContentType: "image/png"}
	}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "Bob", Content: "追加", Attachments: current,
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 || len(translator.contexts[0].History) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if len(translator.contexts[0].History[0].Images) != 0 {
		t.Fatalf("history images should yield to current attachments: %#v", translator.contexts[0].History[0].Images)
	}
	if len(translator.visions[0]) != visionMaxImages {
		t.Fatalf("vision images = %d, want %d", len(translator.visions[0]), visionMaxImages)
	}
}

func seedCountOverflowHistory(t *testing.T, store *Store, now time.Time) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 17; i++ {
		if err := store.SaveMessageLink(ctx, MessageLink{
			SourceMessageID:         snowflakeForTime(now.Add(time.Duration(i)*30*time.Second), uint64(i+1)),
			SourceChannelID:         "ja",
			GroupID:                 "g",
			TargetChannelID:         "en",
			TargetMessageID:         fmt.Sprintf("2000000000000000%02d", i),
			TargetLanguage:          "en",
			SourceAuthorID:          fmt.Sprintf("user-%d", i),
			SourceAuthorDisplayName: fmt.Sprintf("user-%d", i),
			SourceContentSnapshot:   fmt.Sprintf("topic-%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func currentAfterOverflow(now time.Time, extra int) (id string) {
	return snowflakeForTime(now.Add(time.Duration(17+extra)*30*time.Second), uint64(90+extra))
}

// SPEC 3.8
func TestHandleMessageCreateInsertsTopicSummaryOnNextMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	translator := &topicSummaryTranslator{summary: "they are coordinating a delayed shipment"}
	service := NewService(store, &fakeDiscordAPI{}, translator)
	service.runTopicSummary = func(f func()) { f() }
	seedGroup(t, store)
	now := time.Now().UTC()
	seedCountOverflowHistory(t, store, now)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: currentAfterOverflow(now, 0), ChannelID: "ja", GuildID: "guild", AuthorID: "next",
		AuthorDisplayName: "next", Content: "今の状況は？",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if translator.contexts[0].TopicSummary != "" {
		t.Fatalf("overflowing message should not include the new summary: %#v", translator.contexts[0])
	}
	if len(translator.summaryRequests()) != 1 {
		t.Fatalf("summary requests = %#v", translator.summaryRequests())
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: currentAfterOverflow(now, 1), ChannelID: "ja", GuildID: "guild", AuthorID: "later",
		AuthorDisplayName: "later", Content: "了解",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 2 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if translator.contexts[1].TopicSummary != "they are coordinating a delayed shipment" {
		t.Fatalf("next message summary = %q", translator.contexts[1].TopicSummary)
	}
	if len(translator.userPrompts) < 2 || !strings.Contains(translator.userPrompts[1], "<topic_summary>they are coordinating a delayed shipment</topic_summary>") {
		t.Fatalf("next prompt missing frozen topic summary: %#v", translator.userPrompts)
	}
	if strings.Contains(translator.userPrompts[0], "<topic_summary>") {
		t.Fatalf("overflowing prompt should not include topic_summary: %s", translator.userPrompts[0])
	}
}

// SPEC 3.8
func TestHandleMessageCreateDoesNotWaitForInFlightTopicSummary(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	started := make(chan struct{})
	block := make(chan struct{})
	finished := make(chan struct{})
	translator := &topicSummaryTranslator{summary: "they are coordinating a delayed shipment", started: started, block: block}
	service := NewService(store, &fakeDiscordAPI{}, translator)
	service.runTopicSummary = func(f func()) {
		go func() {
			defer close(finished)
			f()
		}()
	}
	seedGroup(t, store)
	now := time.Now().UTC()
	seedCountOverflowHistory(t, store, now)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: currentAfterOverflow(now, 0), ChannelID: "ja", GuildID: "guild", AuthorID: "next",
		AuthorDisplayName: "next", Content: "今の状況は？",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("topic summary did not start")
	}
	if translator.contexts[0].TopicSummary != "" {
		t.Fatal("overflowing message waited for the summary")
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: currentAfterOverflow(now, 1), ChannelID: "ja", GuildID: "guild", AuthorID: "later",
		AuthorDisplayName: "later", Content: "了解",
	}); err != nil {
		t.Fatal(err)
	}
	if translator.contexts[1].TopicSummary != "" {
		t.Fatal("next message waited for an in-flight summary")
	}

	close(block)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("topic summary did not finish")
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: currentAfterOverflow(now, 2), ChannelID: "ja", GuildID: "guild", AuthorID: "after",
		AuthorDisplayName: "after", Content: "続けよう",
	}); err != nil {
		t.Fatal(err)
	}
	if translator.contexts[2].TopicSummary != "they are coordinating a delayed shipment" {
		t.Fatalf("summary after completion = %q", translator.contexts[2].TopicSummary)
	}
}

// SPEC 3.8
func TestHandleMessageCreateRollsPreviousTopicSummaryIntoNextGeneration(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	translator := &topicSummaryTranslator{summary: "updated shipment delay"}
	service := NewService(store, &fakeDiscordAPI{}, translator)
	service.runTopicSummary = func(f func()) { f() }
	seedGroup(t, store)
	now := time.Now().UTC()
	seedCountOverflowHistory(t, store, now)
	if err := store.UpsertTopicSummary(ctx, "guild", "guild:g:group", "old-generation", "earlier they discussed packing"); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: currentAfterOverflow(now, 0), ChannelID: "ja", GuildID: "guild", AuthorID: "next",
		AuthorDisplayName: "next", Content: "今の状況は？",
	}); err != nil {
		t.Fatal(err)
	}
	reqs := translator.summaryRequests()
	if len(reqs) != 1 || !strings.Contains(reqs[0].userPrompt(), "<previous_summary>earlier they discussed packing</previous_summary>") {
		t.Fatalf("previous summary not rolled forward: %#v", reqs)
	}
	if translator.contexts[0].TopicSummary != "" {
		t.Fatalf("overflowing message should not use the previous generation summary: %q", translator.contexts[0].TopicSummary)
	}
}

// SPEC 3.8
func TestHandleMessageCreateOmitsTopicSummaryAcrossIdleGap(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	translator := &topicSummaryTranslator{summary: "should not appear"}
	service := NewService(store, &fakeDiscordAPI{}, translator)
	service.runTopicSummary = func(f func()) { f() }
	seedGroup(t, store)
	now := time.Now().UTC()
	old := snowflakeForTime(now.Add(-21*time.Minute), 1)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: old, SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "old-target", TargetLanguage: "en",
		SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "前のバースト",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTopicSummary(ctx, "guild", "guild:g:group", "ja"+old, "old burst topic"); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: snowflakeForTime(now, 3), ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "新しい話",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if translator.contexts[0].TopicSummary != "" {
		t.Fatalf("idle gap should drop the previous summary: %q", translator.contexts[0].TopicSummary)
	}
	if len(translator.summaryRequests()) != 0 {
		t.Fatalf("idle gap should not summarize: %#v", translator.summaryRequests())
	}
}
