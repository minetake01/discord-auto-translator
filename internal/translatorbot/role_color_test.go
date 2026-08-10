package translatorbot

import "testing"

func TestHighestRoleColorPicksHighestPositionColoredRole(t *testing.T) {
	guildRoles := []GuildRole{
		{ID: "r1", Color: 0xFF0000, Position: 1},
		{ID: "r2", Color: 0x00FF00, Position: 5},
		{ID: "r3", Color: 0x0000FF, Position: 3},
	}
	got := HighestRoleColor([]string{"r1", "r2", "r3"}, guildRoles)
	if got != 0x00FF00 {
		t.Fatalf("got %#06x, want %#06x", got, 0x00FF00)
	}
}

func TestHighestRoleColorSkipsUncoloredRoles(t *testing.T) {
	guildRoles := []GuildRole{
		{ID: "r1", Color: 0, Position: 10},
		{ID: "r2", Color: 0xABCDEF, Position: 2},
	}
	got := HighestRoleColor([]string{"r1", "r2"}, guildRoles)
	if got != 0xABCDEF {
		t.Fatalf("got %#06x, want %#06x", got, 0xABCDEF)
	}
}

func TestHighestRoleColorReturnsZeroWhenNoColoredRoles(t *testing.T) {
	guildRoles := []GuildRole{
		{ID: "r1", Color: 0, Position: 1},
		{ID: "r2", Color: 0, Position: 2},
	}
	got := HighestRoleColor([]string{"r1", "r2"}, guildRoles)
	if got != 0 {
		t.Fatalf("got %#06x, want 0", got)
	}
}

func TestHighestRoleColorIgnoresUnknownRoleIDs(t *testing.T) {
	guildRoles := []GuildRole{
		{ID: "r1", Color: 0x112233, Position: 1},
	}
	got := HighestRoleColor([]string{"missing", "r1"}, guildRoles)
	if got != 0x112233 {
		t.Fatalf("got %#06x, want %#06x", got, 0x112233)
	}
}

func TestHighestRoleColorReturnsZeroForEmptyInput(t *testing.T) {
	if got := HighestRoleColor(nil, nil); got != 0 {
		t.Fatalf("got %#06x, want 0", got)
	}
}
