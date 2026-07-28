package translatorbot

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestFormatPollAnswers(t *testing.T) {
	got := formatPollAnswers(&DiscordPoll{
		Question: "好きな色は？",
		Answers: []DiscordPollAnswer{
			{Text: "赤", Emoji: &DiscordPollEmoji{Name: "🔴"}},
			{Text: "青", Emoji: &DiscordPollEmoji{Name: "blue", ID: "123", Animated: true}},
			{Text: "緑"},
		},
	})
	want := "1. 🔴 赤\n2. <a:blue:123> 青\n3. 緑"
	if got != want {
		t.Fatalf("formatPollAnswers = %q, want %q", got, want)
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
	got := withPollStartedHeader("body", "ja", "guild", "channel", "message", true)
	want := "> -# 投票を開始しました。 · [投票する](https://discord.com/channels/guild/channel/message)\nbody"
	if got != want {
		t.Fatalf("withPollStartedHeader = %q, want %q", got, want)
	}
	if got := withPollStartedHeader("body", "ja", "guild", "channel", "message", false); got != "body" {
		t.Fatalf("without poll = %q", got)
	}
	if !isPseudoReplyLine("> -# 投票を開始しました。 · [投票する](https://discord.com/channels/guild/channel/message)") {
		t.Fatal("poll header should be recognized as a pseudo-reply line")
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
