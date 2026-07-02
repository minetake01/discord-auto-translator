package translatorbot

import (
	"context"
	"strings"
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

func TestHandleAddListRemoveGlossary(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	ctx := context.Background()

	var responses []string
	oldHook := interactionResponseHook
	interactionResponseHook = func(msg string, _ bool) {
		responses = append(responses, msg)
	}
	t.Cleanup(func() {
		interactionResponseHook = oldHook
	})

	member := &discordgo.Member{User: &discordgo.User{ID: "u1"}}
	handler.Handle(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: "g1",
			Member:  member,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "add-glossary",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "term", Type: discordgo.ApplicationCommandOptionString, Value: "NPC"},
					{Name: "translation", Type: discordgo.ApplicationCommandOptionString, Value: "Non-Player Character"},
				},
			},
		},
	})
	if len(responses) != 1 || !strings.Contains(responses[0], "NPC") {
		t.Fatalf("add response = %#v", responses)
	}

	entries, err := store.ListGlossaryEntries(ctx, "g1")
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %#v, err = %v", entries, err)
	}

	responses = nil
	handler.Handle(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: "g1",
			Member:  member,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "list-glossary",
			},
		},
	})
	if len(responses) != 1 || !strings.Contains(responses[0], "Non-Player Character") {
		t.Fatalf("list response = %#v", responses)
	}

	responses = nil
	handler.Handle(nil, &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: "g1",
			Member:  member,
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "remove-glossary",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "term", Type: discordgo.ApplicationCommandOptionString, Value: "NPC"},
				},
			},
		},
	})
	if len(responses) != 1 || !strings.Contains(responses[0], "削除しました") {
		t.Fatalf("remove response = %#v", responses)
	}

	entries, err = store.ListGlossaryEntries(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty", entries)
	}
}
