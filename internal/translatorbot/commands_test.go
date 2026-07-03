package translatorbot

import (
	"context"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func adminCommandMember() *discordgo.Member {
	return &discordgo.Member{
		User:        &discordgo.User{ID: "u1"},
		Permissions: discordgo.PermissionAdministrator,
	}
}

func TestCommandDefaultPermissions(t *testing.T) {
	for _, command := range Commands() {
		if command.Name == viewOriginalCommandName {
			if command.DefaultMemberPermissions != nil {
				t.Fatalf("%s DefaultMemberPermissions = %d, want nil", command.Name, *command.DefaultMemberPermissions)
			}
			continue
		}
		if command.DefaultMemberPermissions == nil || *command.DefaultMemberPermissions != discordgo.PermissionAdministrator {
			t.Fatalf("%s DefaultMemberPermissions = %v, want Administrator", command.Name, command.DefaultMemberPermissions)
		}
	}
}

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

	member := adminCommandMember()
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

func TestHandleListGroups(t *testing.T) {
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

	member := adminCommandMember()
	invoke := func() {
		handler.Handle(nil, &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				Type:    discordgo.InteractionApplicationCommand,
				GuildID: "g1",
				Member:  member,
				Data: discordgo.ApplicationCommandInteractionData{
					Name: "list-groups",
				},
			},
		})
	}

	invoke()
	if len(responses) != 1 || !strings.Contains(responses[0], "翻訳グループが登録されていません") {
		t.Fatalf("empty response = %#v", responses)
	}

	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-ja", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-en", ChannelType: 0, Language: "en", WebhookID: "w2", WebhookToken: "t2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "support", GuildID: "g1", DisplayName: "support", CreatedBy: "u1"}, GroupChannel{
		GroupID: "support", GuildID: "g1", ChannelID: "ch-support", ChannelType: 0, Language: "ja", WebhookID: "w3", WebhookToken: "t3",
	}); err != nil {
		t.Fatal(err)
	}

	responses = nil
	invoke()
	if len(responses) != 1 {
		t.Fatalf("list response count = %d, want 1", len(responses))
	}
	msg := responses[0]
	for _, want := range []string{"翻訳グループ (2)", "**general**", "<#ch-ja>", "(ja)", "<#ch-en>", "(en)", "**support**", "<#ch-support>"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("response missing %q: %s", want, msg)
		}
	}
}

func TestHandleViewOriginalTranslatedMessage(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	ctx := context.Background()

	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-ja", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-en", ChannelType: 0, Language: "en", WebhookID: "w2", WebhookToken: "t2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "orig", SourceChannelID: "ch-ja", GroupID: "general",
		TargetChannelID: "ch-en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorID: "a", SourceContentSnapshot: "Hello from the original message",
	}); err != nil {
		t.Fatal(err)
	}

	var responses []string
	oldHook := interactionResponseHook
	interactionResponseHook = func(msg string, _ bool) {
		responses = append(responses, msg)
	}
	t.Cleanup(func() {
		interactionResponseHook = oldHook
	})

	handler.Handle(nil, viewOriginalInteraction("g1", "ch-en", "translated", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(responses) != 1 {
		t.Fatalf("response count = %d, want 1", len(responses))
	}
	msg := responses[0]
	for _, want := range []string{
		"Go to original message",
		"https://discord.com/channels/g1/ch-ja/orig",
		"> Hello from the original message",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("response missing %q: %s", want, msg)
		}
	}
}

func TestHandleViewOriginalJapaneseChannel(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	ctx := context.Background()

	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-ja", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-en", ChannelType: 0, Language: "en", WebhookID: "w2", WebhookToken: "t2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "orig", SourceChannelID: "ch-ja", GroupID: "general",
		TargetChannelID: "ch-ja", TargetMessageID: "translated-ja", TargetLanguage: "ja",
		SourceAuthorID: "a", SourceContentSnapshot: "original text",
	}); err != nil {
		t.Fatal(err)
	}

	var responses []string
	oldHook := interactionResponseHook
	interactionResponseHook = func(msg string, _ bool) {
		responses = append(responses, msg)
	}
	t.Cleanup(func() {
		interactionResponseHook = oldHook
	})

	handler.Handle(nil, viewOriginalInteraction("g1", "ch-ja", "translated-ja", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(responses) != 1 {
		t.Fatalf("response count = %d, want 1", len(responses))
	}
	if !strings.Contains(responses[0], "原文メッセージへ移動") {
		t.Fatalf("response = %q", responses[0])
	}
}

func TestHandleViewOriginalAlreadyOriginal(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	ctx := context.Background()

	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-en", ChannelType: 0, Language: "en", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "orig", SourceChannelID: "ch-en", GroupID: "general",
		TargetChannelID: "ch-ja", TargetMessageID: "translated", TargetLanguage: "ja",
		SourceAuthorID: "a", SourceContentSnapshot: "original",
	}); err != nil {
		t.Fatal(err)
	}

	var responses []string
	oldHook := interactionResponseHook
	interactionResponseHook = func(msg string, _ bool) {
		responses = append(responses, msg)
	}
	t.Cleanup(func() {
		interactionResponseHook = oldHook
	})

	handler.Handle(nil, viewOriginalInteraction("g1", "ch-en", "orig", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(responses) != 1 || responses[0] != uiStrings["en"][uiKeyAlreadyOriginal] {
		t.Fatalf("response = %#v", responses)
	}
}

func TestHandleViewOriginalNotManaged(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})

	var responses []string
	oldHook := interactionResponseHook
	interactionResponseHook = func(msg string, _ bool) {
		responses = append(responses, msg)
	}
	t.Cleanup(func() {
		interactionResponseHook = oldHook
	})

	handler.Handle(nil, viewOriginalInteraction("g1", "ch-en", "unknown", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(responses) != 1 || responses[0] != uiStrings["en"][uiKeyNotManaged] {
		t.Fatalf("response = %#v", responses)
	}
}

func viewOriginalInteraction(guildID, channelID, messageID string, member *discordgo.Member) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:      discordgo.InteractionApplicationCommand,
			GuildID:   guildID,
			ChannelID: channelID,
			Member:    member,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:        viewOriginalCommandName,
				CommandType: discordgo.MessageApplicationCommand,
				TargetID:    messageID,
			},
		},
	}
}
