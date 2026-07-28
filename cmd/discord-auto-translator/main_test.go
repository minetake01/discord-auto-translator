package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type fakeBedrockWarmer struct {
	warmUp func(context.Context) error
}

func (f fakeBedrockWarmer) WarmUp(ctx context.Context) error {
	return f.warmUp(ctx)
}

func TestAuthorDisplayNameUsesMemberDisplayNameFallbacks(t *testing.T) {
	author := &discordgo.User{Username: "username", GlobalName: "global"}

	tests := []struct {
		name   string
		author *discordgo.User
		member *discordgo.Member
		want   string
	}{
		{
			name:   "member nick",
			author: author,
			member: &discordgo.Member{Nick: "server-nick", User: author},
			want:   "server-nick",
		},
		{
			name:   "user global name",
			author: author,
			member: &discordgo.Member{User: author},
			want:   "global",
		},
		{
			name:   "username",
			author: &discordgo.User{Username: "username"},
			member: &discordgo.Member{User: &discordgo.User{Username: "username"}},
			want:   "username",
		},
		{
			name:   "member nick without member user",
			author: author,
			member: &discordgo.Member{Nick: "server-nick"},
			want:   "server-nick",
		},
		{
			name:   "author fallback when member user is missing",
			author: author,
			member: &discordgo.Member{},
			want:   "global",
		},
		{
			name:   "author fallback when no member",
			author: author,
			want:   "global",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authorDisplayName(tt.author, tt.member); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseStartupOptions(t *testing.T) {
	got, err := parseStartupOptions([]string{"--env-file", "/tmp/bedrock.env", "--bedrock-prewarm"})
	if err != nil {
		t.Fatal(err)
	}
	if got.envFile != "/tmp/bedrock.env" || !got.bedrockPrewarm {
		t.Fatalf("options = %#v", got)
	}

	defaults, err := parseStartupOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.envFile != ".env" || defaults.bedrockPrewarm {
		t.Fatalf("defaults = %#v", defaults)
	}

	for _, args := range [][]string{{"--env-file", ""}, {"unexpected"}, {"--unknown"}} {
		if _, err := parseStartupOptions(args); err == nil {
			t.Fatalf("parseStartupOptions(%q) succeeded, want error", args)
		}
	}
}

func TestPrewarmBedrockUsesFiveMinuteDeadlineAndReturnsErrors(t *testing.T) {
	started := time.Now()
	if err := prewarmBedrock(context.Background(), fakeBedrockWarmer{warmUp: func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("prewarm context has no deadline")
		}
		if remaining := deadline.Sub(started); remaining < 4*time.Minute+59*time.Second || remaining > 5*time.Minute+time.Second {
			t.Fatalf("prewarm deadline remaining = %s", remaining)
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}

	want := errors.New("access denied")
	err := prewarmBedrock(context.Background(), fakeBedrockWarmer{warmUp: func(context.Context) error { return want }})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err = prewarmBedrock(canceled, fakeBedrockWarmer{warmUp: func(ctx context.Context) error { return ctx.Err() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestReferencedMessageFieldsIgnoresForward(t *testing.T) {
	id, channelID, content := referencedMessageFields(
		&discordgo.MessageReference{Type: discordgo.MessageReferenceTypeForward, MessageID: "ref", ChannelID: "ch"},
		&discordgo.Message{ID: "ref", Content: "not a reply"},
	)
	if id != "" || channelID != "" || content != "" {
		t.Fatalf("forward leaked into reply fields: %q %q %q", id, channelID, content)
	}
}

func TestForwardedMessageFields(t *testing.T) {
	got, err := forwardedMessageFields(
		&discordgo.MessageReference{Type: discordgo.MessageReferenceTypeForward, MessageID: "message", ChannelID: "channel", GuildID: "origin-guild"},
		[]discordgo.MessageSnapshot{{Message: &discordgo.Message{
			Content:      "snapshot body",
			Attachments:  []*discordgo.MessageAttachment{{URL: "https://cdn.discordapp.com/a.png", Filename: "a.png"}},
			StickerItems: []*discordgo.StickerItem{{ID: "sticker", Name: "wave", FormatType: discordgo.StickerFormatTypePNG}},
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.MessageID != "message" || got.ChannelID != "channel" || got.GuildID != "origin-guild" || got.Content != "snapshot body" {
		t.Fatalf("unexpected forward: %#v", got)
	}
	if len(got.Attachments) != 1 || len(got.Stickers) != 1 {
		t.Fatalf("snapshot assets were not mapped: %#v", got)
	}
}

func TestForwardedMessageFieldsRejectsMalformedSnapshots(t *testing.T) {
	ref := &discordgo.MessageReference{Type: discordgo.MessageReferenceTypeForward, MessageID: "message", ChannelID: "channel"}
	for name, snapshots := range map[string][]discordgo.MessageSnapshot{
		"missing":     nil,
		"nil message": {{Message: nil}},
		"multiple":    {{Message: &discordgo.Message{}}, {Message: &discordgo.Message{}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := forwardedMessageFields(ref, snapshots); err == nil || got != nil {
				t.Fatalf("got %#v, err %v", got, err)
			}
		})
	}
}
