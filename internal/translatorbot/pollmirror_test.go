package translatorbot

// SPEC 3.2 polls

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
