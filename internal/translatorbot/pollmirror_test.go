package translatorbot

// SPEC 3.2 polls

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// SPEC 3.2 polls
func TestHandleMessageCreateMirrorsPollAsEmbedWithVoteLink(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000050", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		AuthorRoleColor: 0xabcdef,
		Poll: &DiscordPoll{
			Question: "好きな色は？",
			Answers: []DiscordPollAnswer{
				{Text: "赤", Emoji: &DiscordPollEmoji{Name: "🔴"}},
				{Text: "青"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 0 || len(translator.pollContexts) != 1 {
		t.Fatalf("expected one poll translation, contexts=%#v pollContexts=%#v", translator.contexts, translator.pollContexts)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	sourceURL := MessageJumpURL("guild", "ja", "100000000000000050")
	wantHeader := fmt.Sprintf("> -# %s · [%s](%s)",
		localizedUIString("en", uiKeyPollStarted),
		localizedUIString("en", uiKeyPollVote),
		sourceURL,
	)
	if got := discord.sent[0].Content; got != wantHeader {
		t.Fatalf("content = %q, want %q", got, wantHeader)
	}
	if strings.Contains(discord.sent[0].Content, MessageJumpURL("guild", "en", "100000000000000050")) {
		t.Fatal("vote link must not point at the target channel")
	}
	if len(discord.sent[0].Embeds) != 1 || discord.sent[0].Embeds[0] == nil {
		t.Fatalf("embeds: %#v", discord.sent[0].Embeds)
	}
	embed := discord.sent[0].Embeds[0]
	if embed.Title != "[en] 好きな色は？" || embed.Description != "1. 🔴 [en] 赤\n2. [en] 青" || embed.Color != 0xabcdef {
		t.Fatalf("embed = %#v", embed)
	}
	links, err := store.MessageTargets(ctx, "ja", "100000000000000050")
	if err != nil || len(links) != 1 {
		t.Fatalf("links: %#v err=%v", links, err)
	}
	wantSnapshot := "好きな色は？\n1. 🔴 赤\n2. 青"
	if links[0].SourceContentSnapshot != wantSnapshot {
		t.Fatalf("snapshot = %q, want %q", links[0].SourceContentSnapshot, wantSnapshot)
	}
}

// SPEC 3.2 polls
func TestHandleMessageCreateKeepsPollVoteLinkAfterDiscordRefRewrite(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	sourcePollURL := MessageJumpURL("guild", "ja", "100000000000000052")
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000052", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Content: "see " + MessageJumpURL("guild", "ja", "100000000000000001"),
		Poll: &DiscordPoll{
			Question: "Q",
			Answers:  []DiscordPollAnswer{{Text: "A"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	got := discord.sent[0].Content
	wantHeader := fmt.Sprintf("> -# %s · [%s](%s)",
		localizedUIString("en", uiKeyPollStarted),
		localizedUIString("en", uiKeyPollVote),
		sourcePollURL,
	)
	if !strings.HasPrefix(got, wantHeader) {
		t.Fatalf("missing source vote header: %q", got)
	}
	// Body may rewrite the embedded message URL; the poll vote header must stay on the source poll.
	if strings.Count(got, sourcePollURL) < 1 {
		t.Fatalf("vote link missing from content: %q", got)
	}
	if len(discord.sent[0].Embeds) != 1 || discord.sent[0].Embeds[0] == nil || discord.sent[0].Embeds[0].Title != "[en] Q" {
		t.Fatalf("embeds: %#v", discord.sent[0].Embeds)
	}
}

// SPEC 3.2 polls
func TestFormatTranslatedPollAnswers(t *testing.T) {
	poll := &DiscordPoll{
		Question: "好きな色は？",
		Answers: []DiscordPollAnswer{
			{Text: "赤", Emoji: &DiscordPollEmoji{Name: "🔴"}},
			{Text: "青", Emoji: &DiscordPollEmoji{Name: "blue", ID: "123", Animated: true}},
			{Text: "緑"},
		},
	}
	got := formatTranslatedPollAnswers(poll, []string{"Red", "Blue", "Green"})
	want := "1. 🔴 Red\n2. <a:blue:123> Blue\n3. Green"
	if got != want {
		t.Fatalf("formatTranslatedPollAnswers = %q, want %q", got, want)
	}
}

// SPEC 3.2 polls
func TestFormatPollSnapshot(t *testing.T) {
	got := formatPollSnapshot(&DiscordPoll{
		Question: "Q",
		Answers:  []DiscordPollAnswer{{Text: "A"}},
	})
	want := "Q\n1. A"
	if got != want {
		t.Fatalf("formatPollSnapshot = %q, want %q", got, want)
	}
}

// SPEC 3.2 polls
func TestBuildPollEmbed(t *testing.T) {
	embed := buildPollEmbed("Question", "1. A\n2. B", 0x112233)
	if embed == nil || embed.Title != "Question" || embed.Description != "1. A\n2. B" || embed.Color != 0x112233 {
		t.Fatalf("embed = %#v", embed)
	}
	if buildPollEmbed("", "", 0) != nil {
		t.Fatal("empty embed should be nil")
	}
	long := strings.Repeat("あ", discordEmbedTitleLimit+10)
	truncated := buildPollEmbed(long, "answers", 0)
	if truncated == nil || len([]rune(truncated.Title)) != discordEmbedTitleLimit {
		t.Fatalf("title runes = %d, want %d", len([]rune(truncated.Title)), discordEmbedTitleLimit)
	}
}

// SPEC 3.2 polls
func TestWithPollStartedHeader(t *testing.T) {
	const (
		body      = "body"
		guildID   = "guild"
		channelID = "channel"
		messageID = "message"
	)
	got := withPollStartedHeader(body, "ja", guildID, channelID, messageID, true)
	if !strings.HasPrefix(got, "> -# ") {
		t.Fatalf("poll header should start with blockquote prefix, got %q", got)
	}
	jumpURL := MessageJumpURL(guildID, channelID, messageID)
	if !strings.Contains(got, "](https://discord.com/channels/"+guildID+"/"+channelID+"/"+messageID+")") &&
		!strings.Contains(got, "]("+jumpURL+")") {
		t.Fatalf("poll header should contain markdown link to source message, got %q", got)
	}
	if !strings.HasSuffix(got, "\n"+body) {
		t.Fatalf("original body should appear after header, got %q", got)
	}
	if got := withPollStartedHeader(body, "ja", guildID, channelID, messageID, false); got != body {
		t.Fatalf("without poll = %q", got)
	}
	headerLine := strings.SplitN(got, "\n", 2)[0]
	if !isPseudoReplyLine(headerLine) {
		t.Fatalf("poll header should be recognized as a pseudo-reply line: %q", headerLine)
	}
}

// SPEC 3.2 polls
func TestContentWithEmbedQuoteText(t *testing.T) {
	got := contentWithEmbedQuoteText(
		"> -# 投票を開始しました。 · [投票する](https://discord.com/channels/g/c/m)",
		[]*discordgo.MessageEmbed{{Title: "好きな色は？", Description: "1. 赤"}},
	)
	want := "> -# 投票を開始しました。 · [投票する](https://discord.com/channels/g/c/m)\n好きな色は？"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if first := firstLineWithoutPseudoReply(got); first != "好きな色は？" {
		t.Fatalf("quote snippet = %q", first)
	}
}

// SPEC 3.2 polls
func TestParsePollTranslationResponse(t *testing.T) {
	protector := NewProtector(NameMaps{})
	raw := `{"translations":[{"language":"en","question":"Favorite?","answers":["Red","Blue"]}]}`
	got, err := parsePollTranslationResponse(raw, []string{"en"}, 2, protector)
	if err != nil {
		t.Fatal(err)
	}
	if got["en"].Question != "Favorite?" || len(got["en"].Answers) != 2 || got["en"].Answers[0] != "Red" {
		t.Fatalf("got %#v", got)
	}
	_, err = parsePollTranslationResponse(`{"translations":[{"language":"en","question":"Q","answers":["only-one"]}]}`, []string{"en"}, 2, protector)
	if err == nil {
		t.Fatal("expected answer count mismatch")
	}
}

// SPEC 3.2 poll results
func TestPollResultPercent(t *testing.T) {
	if got := pollResultPercent(3, 5); got != 60 {
		t.Fatalf("3/5 = %d, want 60", got)
	}
	if got := pollResultPercent(1, 3); got != 33 {
		t.Fatalf("1/3 = %d, want 33", got)
	}
	if got := pollResultPercent(1, 0); got != 0 {
		t.Fatalf("div zero = %d", got)
	}
}

// SPEC 3.2 poll results
func TestHandleMessageCreateCachesPollTranslationsAndMirrorsResult(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		messages: map[string]DiscordFetchedMessage{
			"en\x00translated-poll": {Content: "> -# A poll has started. · [Vote](https://discord.com/channels/guild/ja/100000000000000050)\nFavorite color?"},
		},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	expiry := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000050", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Poll: &DiscordPoll{
			Question: "好きな色は？",
			Answers: []DiscordPollAnswer{
				{Text: "赤", Emoji: &DiscordPollEmoji{Name: "🔴"}},
				{Text: "青"},
			},
			Expiry: &expiry,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	answers, ok, err := store.PollTranslatedAnswers(ctx, "ja", "100000000000000050", "en")
	if err != nil || !ok || len(answers) != 2 || answers[0] != "[en] 赤" {
		t.Fatalf("cache = %#v ok=%v err=%v", answers, ok, err)
	}

	discord.sent = nil
	translator.pollContexts = nil
	translator.contexts = nil
	_ = store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000050", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated-poll", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "好きな色は？",
	})

	err = service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000060", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ReferencedMessageID: "100000000000000050", ReferencedMessageChannelID: "ja",
		PollResult: &DiscordPollResult{
			HasEmbed: true, VictorAnswerID: 1, VictorAnswerText: "赤",
			VictorEmoji: &DiscordPollEmoji{Name: "🔴"},
			VictorAnswerVotes: 3, TotalVotes: 5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 0 || len(translator.pollContexts) != 0 {
		t.Fatalf("poll result must not call translator: %#v %#v", translator.contexts, translator.pollContexts)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	wantBody := localizedUIString("en", uiKeyPollEnded) + "\n" +
		localizedUIStringf("en", uiKeyPollResultVictor, "🔴 [en] 赤", "60")
	if !strings.Contains(discord.sent[0].Content, wantBody) {
		t.Fatalf("content = %q, want body %q", discord.sent[0].Content, wantBody)
	}
	if !strings.HasPrefix(discord.sent[0].Content, "> -#") {
		t.Fatalf("expected pseudo-reply prefix: %q", discord.sent[0].Content)
	}
	if _, ok, err := store.PollTranslatedAnswers(ctx, "ja", "100000000000000050", "en"); err != nil || ok {
		t.Fatalf("cache should be deleted after result, ok=%v err=%v", ok, err)
	}
}

// SPEC 3.2 poll results
func TestHandleMessageCreatePollResultNoWinner(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000061", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ReferencedMessageID: "100000000000000050", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "好きな色は？",
		PollResult:               &DiscordPollResult{HasEmbed: true, TotalVotes: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	want := localizedUIString("en", uiKeyPollEnded) + "\n" + localizedUIString("en", uiKeyPollResultNoWinner)
	if !strings.Contains(discord.sent[0].Content, want) {
		t.Fatalf("content = %q, want %q", discord.sent[0].Content, want)
	}
}

// SPEC 3.2 poll results
func TestHandleMessageCreatePollResultWithoutEmbed(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000062", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ReferencedMessageID: "100000000000000050", ReferencedMessageContent: "poll",
		PollResult: &DiscordPollResult{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	got := discord.sent[0].Content
	ended := localizedUIString("en", uiKeyPollEnded)
	if !strings.Contains(got, ended) || strings.Contains(got, localizedUIString("en", uiKeyPollResultNoWinner)) {
		t.Fatalf("content = %q", got)
	}
}

// SPEC 3.2 poll results
func TestHandleMessageCreateSkipsPollCacheWithoutExpiry(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	service := NewService(store, &fakeDiscordAPI{}, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000063", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Poll: &DiscordPoll{Question: "Q", Answers: []DiscordPollAnswer{{Text: "A"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.PollTranslatedAnswers(ctx, "ja", "100000000000000063", "en"); err != nil || ok {
		t.Fatalf("expected no cache without expiry, ok=%v err=%v", ok, err)
	}
}

func TestPurgeExpiredPollTranslationCache(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	past := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	if err := store.SavePollTranslationCache(ctx, "ja", "100000000000000070", "en", []string{"Old"}, past); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePollTranslationCache(ctx, "ja", "100000000000000071", "en", []string{"New"}, future); err != nil {
		t.Fatal(err)
	}
	n, err := store.PurgeExpiredPollTranslationCache(ctx, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("purged=%d err=%v", n, err)
	}
	if _, ok, _ := store.PollTranslatedAnswers(ctx, "ja", "100000000000000070", "en"); ok {
		t.Fatal("expired row remained")
	}
	if _, ok, err := store.PollTranslatedAnswers(ctx, "ja", "100000000000000071", "en"); err != nil || !ok {
		t.Fatal("future row was purged")
	}
}
