package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestMemberRoleColorPicksHighestPositionColoredRole(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	if err := session.State.GuildAdd(&discordgo.Guild{
		ID: "guild-1",
		Roles: []*discordgo.Role{
			{ID: "r1", Color: 0xFF0000, Position: 1},
			{ID: "r2", Color: 0x00FF00, Position: 5},
		},
	}); err != nil {
		t.Fatal(err)
	}
	member := &discordgo.Member{Roles: []string{"r1", "r2"}}
	got := memberRoleColor(session, "guild-1", member)
	if got != 0x00FF00 {
		t.Fatalf("got %#06x, want %#06x", got, 0x00FF00)
	}
}

func TestMemberRoleColorReturnsZeroWithoutMember(t *testing.T) {
	if got := memberRoleColor(&discordgo.Session{}, "guild-1", nil); got != 0 {
		t.Fatalf("got %#06x, want 0", got)
	}
}
