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
