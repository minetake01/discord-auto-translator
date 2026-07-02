package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

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

func TestAttachmentsFromDiscordMapsWebhookFileFields(t *testing.T) {
	got := attachmentsFromDiscord([]*discordgo.MessageAttachment{
		nil,
		{URL: "https://cdn.discordapp.com/a.png", Filename: "a.png", ContentType: "image/png"},
	})

	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got[0].URL != "https://cdn.discordapp.com/a.png" || got[0].Filename != "a.png" || got[0].ContentType != "image/png" {
		t.Fatalf("unexpected attachment mapping: %#v", got[0])
	}
}

func TestReferencedMessageFields(t *testing.T) {
	id, channelID, authorID, content := referencedMessageFields(
		&discordgo.MessageReference{MessageID: "ref", ChannelID: "ch"},
		&discordgo.Message{
			ID: "ref", ChannelID: "ch", Content: "quoted",
			Author: &discordgo.User{ID: "author"},
		},
	)
	if id != "ref" || channelID != "ch" || authorID != "author" || content != "quoted" {
		t.Fatalf("got %q %q %q %q", id, channelID, authorID, content)
	}
}

func TestStickersFromDiscordMapsStickerFields(t *testing.T) {
	got := stickersFromDiscord([]*discordgo.StickerItem{
		nil,
		{ID: "1", Name: "wave", FormatType: discordgo.StickerFormatTypePNG},
	})
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got[0].ID != "1" || got[0].Name != "wave" || got[0].FormatType != 1 {
		t.Fatalf("unexpected sticker mapping: %#v", got[0])
	}
}
