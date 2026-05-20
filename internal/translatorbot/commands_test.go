package translatorbot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestOptionChannelUsesSelectedChannelID(t *testing.T) {
	options := []*discordgo.ApplicationCommandInteractionDataOption{
		{
			Name:  "channel",
			Type:  discordgo.ApplicationCommandOptionChannel,
			Value: "selected-channel",
		},
	}

	if got := optionChannel(options, "channel", "current-channel"); got != "selected-channel" {
		t.Fatalf("got %q, want selected-channel", got)
	}
}

func TestOptionChannelFallsBackToCurrentChannel(t *testing.T) {
	if got := optionChannel(nil, "channel", "current-channel"); got != "current-channel" {
		t.Fatalf("got %q, want current-channel", got)
	}
}
