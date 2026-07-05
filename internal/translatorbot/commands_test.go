package translatorbot

import (
	"context"
	"fmt"
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

// captureResponses replaces the handler's responder and returns a pointer to
// the slice that collects every response message.
func captureResponses(handler *CommandHandler) *[]string {
	responses := &[]string{}
	handler.respond = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, msg string, _ bool) {
		*responses = append(*responses, msg)
	}
	return responses
}

func slashCommandInteraction(guildID, name string, options []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:    discordgo.InteractionApplicationCommand,
			GuildID: guildID,
			Member:  adminCommandMember(),
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    name,
				Options: options,
			},
		},
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

func TestAddGlossaryAlwaysIncludeOption(t *testing.T) {
	for _, command := range Commands() {
		if command.Name != "add-glossary" {
			continue
		}
		for _, option := range command.Options {
			if option.Name == "always_include" {
				if option.Type != discordgo.ApplicationCommandOptionBoolean || option.Required {
					t.Fatalf("always_include = %#v", option)
				}
				if optionBool(nil, "always_include") {
					t.Fatal("omitted always_include must default to false")
				}
				return
			}
		}
		t.Fatal("add-glossary is missing always_include")
	}
	t.Fatal("add-glossary command not found")
}

func TestAddGlossaryAttributeOptionAndSuggestions(t *testing.T) {
	for _, command := range Commands() {
		if command.Name != "add-glossary" {
			continue
		}
		for _, option := range command.Options {
			if option.Name == "attribute" {
				if option.Type != discordgo.ApplicationCommandOptionString || option.Required || !option.Autocomplete {
					t.Fatalf("attribute = %#v", option)
				}
				got := glossaryAttributeSuggestions("略", 25)
				if len(got) != 1 || got[0] != "略語" {
					t.Fatalf("suggestions = %#v", got)
				}
				return
			}
		}
		t.Fatal("add-glossary is missing attribute")
	}
	t.Fatal("add-glossary command not found")
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
	responses := captureResponses(handler)

	handler.Handle(nil, slashCommandInteraction("g1", "add-glossary", []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "term", Type: discordgo.ApplicationCommandOptionString, Value: "NPC"},
		{Name: "translation", Type: discordgo.ApplicationCommandOptionString, Value: "Non-Player Character"},
		{Name: "attribute", Type: discordgo.ApplicationCommandOptionString, Value: "略語"},
		{Name: "always_include", Type: discordgo.ApplicationCommandOptionBoolean, Value: true},
	}))
	if len(*responses) != 1 || !strings.Contains((*responses)[0], "NPC") {
		t.Fatalf("add response = %#v", *responses)
	}

	entries, err := store.ListGlossaryEntries(ctx, "g1")
	if err != nil || len(entries) != 1 || entries[0].Attribute != "略語" || !entries[0].AlwaysInclude {
		t.Fatalf("entries = %#v, err = %v", entries, err)
	}

	*responses = nil
	handler.Handle(nil, slashCommandInteraction("g1", "list-glossary", nil))
	alwaysLabel := localizedUIString("en", uiKeyGlossaryModeAlways)
	if len(*responses) != 1 || !strings.Contains((*responses)[0], "Non-Player Character") || !strings.Contains((*responses)[0], "略語") || !strings.Contains((*responses)[0], alwaysLabel) {
		t.Fatalf("list response = %#v", *responses)
	}

	*responses = nil
	handler.Handle(nil, slashCommandInteraction("g1", "remove-glossary", []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "term", Type: discordgo.ApplicationCommandOptionString, Value: "NPC"},
	}))
	want := localizedUIStringf("en", uiKeyGlossaryRemoved, "NPC")
	if len(*responses) != 1 || (*responses)[0] != want {
		t.Fatalf("remove response = %#v, want %q", *responses, want)
	}

	entries, err = store.ListGlossaryEntries(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty", entries)
	}
}

func TestHandleCommandRespondsInInteractionLocale(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	responses := captureResponses(handler)

	interaction := slashCommandInteraction("g1", "delete-group", []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "group", Type: discordgo.ApplicationCommandOptionString, Value: "missing"},
	})
	interaction.Locale = discordgo.Japanese
	handler.Handle(nil, interaction)

	want := localizedUIStringf("ja", uiKeyGroupNotFound, "missing")
	if len(*responses) != 1 || (*responses)[0] != want {
		t.Fatalf("response = %#v, want %q", *responses, want)
	}
}

func TestHandleListGlossaryTruncatesAtDiscordLimit(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	ctx := context.Background()
	for n := 0; n < glossaryMaxEntries; n++ {
		term := fmt.Sprintf("term-%02d", n)
		if err := store.UpsertGlossaryEntry(ctx, "g1", term, strings.Repeat("訳", 100), "専門用語", "u1", false); err != nil {
			t.Fatal(err)
		}
	}

	responses := captureResponses(handler)
	handler.Handle(nil, slashCommandInteraction("g1", "list-glossary", nil))
	if len(*responses) != 1 {
		t.Fatalf("responses = %#v", *responses)
	}
	response := (*responses)[0]
	if len(response) > discordMessageContentLimit {
		t.Fatalf("response length = %d", len(response))
	}
	if !strings.Contains(response, localizedUIString("en", uiKeyGlossaryTruncated)) {
		t.Fatalf("response was not marked truncated: %q", response)
	}
}

func TestHandleListGroups(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	ctx := context.Background()
	responses := captureResponses(handler)

	invoke := func() {
		handler.Handle(nil, slashCommandInteraction("g1", "list-groups", nil))
	}

	invoke()
	if len(*responses) != 1 || (*responses)[0] != localizedUIString("en", uiKeyNoGroups) {
		t.Fatalf("empty response = %#v", *responses)
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

	*responses = nil
	invoke()
	if len(*responses) != 1 {
		t.Fatalf("list response count = %d, want 1", len(*responses))
	}
	msg := (*responses)[0]
	header := localizedUIStringf("en", uiKeyGroupsHeader, 2)
	for _, want := range []string{header, "**general**", "<#ch-ja>", "(ja)", "<#ch-en>", "(en)", "**support**", "<#ch-support>"} {
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
		SourceMessageID: "100000000000000002", SourceChannelID: "ch-ja", GroupID: "general",
		TargetChannelID: "ch-en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorID: "a", SourceContentSnapshot: "Hello from the original message",
	}); err != nil {
		t.Fatal(err)
	}

	responses := captureResponses(handler)
	handler.Handle(nil, viewOriginalInteraction("g1", "ch-en", "translated", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(*responses) != 1 {
		t.Fatalf("response count = %d, want 1", len(*responses))
	}
	msg := (*responses)[0]
	for _, want := range []string{
		"Go to original message",
		"https://discord.com/channels/g1/ch-ja/100000000000000002",
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
		SourceMessageID: "100000000000000002", SourceChannelID: "ch-ja", GroupID: "general",
		TargetChannelID: "ch-ja", TargetMessageID: "translated-ja", TargetLanguage: "ja",
		SourceAuthorID: "a", SourceContentSnapshot: "original text",
	}); err != nil {
		t.Fatal(err)
	}

	responses := captureResponses(handler)
	handler.Handle(nil, viewOriginalInteraction("g1", "ch-ja", "translated-ja", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(*responses) != 1 {
		t.Fatalf("response count = %d, want 1", len(*responses))
	}
	if !strings.Contains((*responses)[0], "原文メッセージへ移動") {
		t.Fatalf("response = %q", (*responses)[0])
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
		SourceMessageID: "100000000000000002", SourceChannelID: "ch-en", GroupID: "general",
		TargetChannelID: "ch-ja", TargetMessageID: "translated", TargetLanguage: "ja",
		SourceAuthorID: "a", SourceContentSnapshot: "100000000000000003",
	}); err != nil {
		t.Fatal(err)
	}

	responses := captureResponses(handler)
	handler.Handle(nil, viewOriginalInteraction("g1", "ch-en", "100000000000000002", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(*responses) != 1 || (*responses)[0] != uiStrings["en"][uiKeyAlreadyOriginal] {
		t.Fatalf("response = %#v", *responses)
	}
}

func TestHandleViewOriginalNotManaged(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})

	responses := captureResponses(handler)
	handler.Handle(nil, viewOriginalInteraction("g1", "ch-en", "100000000000000011", &discordgo.Member{User: &discordgo.User{ID: "u1"}}))
	if len(*responses) != 1 || (*responses)[0] != uiStrings["en"][uiKeyNotManaged] {
		t.Fatalf("response = %#v", *responses)
	}
}

func TestHandleSetStyle(t *testing.T) {
	store := newTestStore(t)
	handler := NewCommandHandler(store, &fakeDiscordAPI{})
	ctx := context.Background()

	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "ch-ja", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}

	responses := captureResponses(handler)
	invoke := func(options []*discordgo.ApplicationCommandInteractionDataOption) {
		handler.Handle(nil, slashCommandInteraction("g1", "set-style", options))
	}

	invoke([]*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "group", Type: discordgo.ApplicationCommandOptionString, Value: "general"},
	})
	if len(*responses) != 1 || (*responses)[0] != localizedUIString("en", uiKeyStyleNoneSpecified) {
		t.Fatalf("missing options response = %#v", *responses)
	}

	*responses = nil
	invoke([]*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "group", Type: discordgo.ApplicationCommandOptionString, Value: "general"},
		{Name: "preset", Type: discordgo.ApplicationCommandOptionString, Value: "formal"},
		{Name: "custom", Type: discordgo.ApplicationCommandOptionString, Value: "短く"},
	})
	if len(*responses) != 1 || (*responses)[0] != localizedUIString("en", uiKeyStyleBothSpecified) {
		t.Fatalf("both options response = %#v", *responses)
	}

	*responses = nil
	invoke([]*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "group", Type: discordgo.ApplicationCommandOptionString, Value: "general"},
		{Name: "preset", Type: discordgo.ApplicationCommandOptionString, Value: "netslang"},
	})
	if len(*responses) != 1 || !strings.Contains((*responses)[0], "`netslang`") {
		t.Fatalf("preset response = %#v", *responses)
	}
	preset, custom, err := store.GroupStyle(ctx, "g1", "general")
	if err != nil {
		t.Fatal(err)
	}
	if preset != "netslang" || custom != "" {
		t.Fatalf("stored preset = %q custom = %q", preset, custom)
	}

	*responses = nil
	invoke([]*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "group", Type: discordgo.ApplicationCommandOptionString, Value: "general"},
		{Name: "custom", Type: discordgo.ApplicationCommandOptionString, Value: "敬語を使わないで"},
	})
	if len(*responses) != 1 || (*responses)[0] != localizedUIStringf("en", uiKeyStyleCustomSet, "general", "敬語を使わないで") {
		t.Fatalf("custom response = %#v", *responses)
	}
	preset, custom, err = store.GroupStyle(ctx, "g1", "general")
	if err != nil {
		t.Fatal(err)
	}
	if preset != "" || custom != "敬語を使わないで" {
		t.Fatalf("stored custom = preset %q custom %q", preset, custom)
	}

	*responses = nil
	invoke([]*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "group", Type: discordgo.ApplicationCommandOptionString, Value: "general"},
		{Name: "preset", Type: discordgo.ApplicationCommandOptionString, Value: StylePresetDefault},
	})
	if len(*responses) != 1 || (*responses)[0] != localizedUIStringf("en", uiKeyStyleReset, "general") {
		t.Fatalf("reset response = %#v", *responses)
	}
	preset, custom, err = store.GroupStyle(ctx, "g1", "general")
	if err != nil {
		t.Fatal(err)
	}
	if preset != "" || custom != "" {
		t.Fatalf("reset style = preset %q custom %q", preset, custom)
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
