package translatorbot

import (
	"testing"
)

func TestFormatPollBody(t *testing.T) {
	got := formatPollBody(&DiscordPoll{
		Question: "好きな色は？",
		Answers: []DiscordPollAnswer{
			{Text: "赤", Emoji: &DiscordPollEmoji{Name: "🔴"}},
			{Text: "青", Emoji: &DiscordPollEmoji{Name: "blue", ID: "123", Animated: true}},
			{Text: "緑"},
		},
	})
	want := "## 好きな色は？\n1. 🔴 赤\n2. <a:blue:123> 青\n3. 緑"
	if got != want {
		t.Fatalf("formatPollBody = %q, want %q", got, want)
	}
}

func TestMessageBodyForMirrorCombinesContentAndPoll(t *testing.T) {
	got := messageBodyForMirror(DiscordMessage{
		Content: "見てね",
		Poll: &DiscordPoll{
			Question: "Q",
			Answers:  []DiscordPollAnswer{{Text: "A"}},
		},
	})
	want := "見てね\n\n## Q\n1. A"
	if got != want {
		t.Fatalf("messageBodyForMirror = %q, want %q", got, want)
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
