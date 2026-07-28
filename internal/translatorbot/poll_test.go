package translatorbot

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

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