package translatorbot

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func Commands() []*discordgo.ApplicationCommand {
	channelTypes := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildForum,
		discordgo.ChannelTypeGuildMedia,
	}
	return []*discordgo.ApplicationCommand{
		{
			Name:        "new-channel",
			Description: "Create a translation group from this channel or another channel",
			Options: []*discordgo.ApplicationCommandOption{
				{Name: "language", Description: "BCP-47 language code", Type: discordgo.ApplicationCommandOptionString, Required: true, Autocomplete: true},
				{Name: "channel", Description: "Channel or forum to register", Type: discordgo.ApplicationCommandOptionChannel, Required: false, ChannelTypes: channelTypes},
				{Name: "group", Description: "Short group identifier", Type: discordgo.ApplicationCommandOptionString, Required: false},
			},
		},
		{
			Name:        "join-channel",
			Description: "Join this channel or another channel to a translation group",
			Options: []*discordgo.ApplicationCommandOption{
				{Name: "group", Description: "Existing translation group", Type: discordgo.ApplicationCommandOptionString, Required: true, Autocomplete: true},
				{Name: "language", Description: "BCP-47 language code", Type: discordgo.ApplicationCommandOptionString, Required: true, Autocomplete: true},
				{Name: "channel", Description: "Channel or forum to join", Type: discordgo.ApplicationCommandOptionChannel, Required: false, ChannelTypes: channelTypes},
			},
		},
		{
			Name:        "leave-channel",
			Description: "Remove this channel or another channel from a translation group",
			Options: []*discordgo.ApplicationCommandOption{
				{Name: "group", Description: "Existing translation group", Type: discordgo.ApplicationCommandOptionString, Required: true, Autocomplete: true},
				{Name: "channel", Description: "Channel or forum to remove", Type: discordgo.ApplicationCommandOptionChannel, Required: false, ChannelTypes: channelTypes},
			},
		},
		{
			Name:        "delete-group",
			Description: "Delete a translation group",
			Options: []*discordgo.ApplicationCommandOption{
				{Name: "group", Description: "Existing translation group", Type: discordgo.ApplicationCommandOptionString, Required: true, Autocomplete: true},
			},
		},
	}
}

type CommandHandler struct {
	store *Store
	api   DiscordAPI
}

func NewCommandHandler(store *Store, api DiscordAPI) *CommandHandler {
	return &CommandHandler{store: store, api: api}
}

func (h *CommandHandler) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand && i.Type != discordgo.InteractionApplicationCommandAutocomplete {
		return
	}
	data := i.ApplicationCommandData()
	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		h.handleAutocomplete(s, i, data)
		return
	}
	switch data.Name {
	case "new-channel":
		h.handleNewChannel(s, i, data)
	case "join-channel":
		h.handleJoinChannel(s, i, data)
	case "leave-channel":
		h.handleLeaveChannel(s, i, data)
	case "delete-group":
		h.handleDeleteGroup(s, i, data)
	}
}

func (h *CommandHandler) handleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	focused := focusedOption(data.Options)
	var choices []*discordgo.ApplicationCommandOptionChoice
	if focused != nil && focused.Name == "language" {
		for _, code := range LanguageSuggestions(focused.StringValue(), 25) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: code, Value: code})
		}
	}
	if focused != nil && focused.Name == "group" {
		groups, err := h.store.Groups(context.Background(), i.GuildID, focused.StringValue(), 25)
		if err == nil {
			for _, g := range groups {
				choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: g.DisplayName, Value: g.ID})
			}
		}
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

func (h *CommandHandler) handleNewChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	language := normalizeLanguage(optionString(data.Options, "language"))
	if !IsValidLanguageCode(language) {
		respond(s, i, "言語は `en`, `ja`, `zh-CN`, `pt-BR` のようなBCP-47形式の短いコードで指定してください。", true)
		return
	}
	channelID := optionChannel(data.Options, "channel", i.ChannelID)
	groupID := optionString(data.Options, "group")
	ch, err := s.Channel(channelID)
	if err != nil {
		respond(s, i, "チャンネルを取得できませんでした: "+err.Error(), true)
		return
	}
	if !allowedChannelType(ch.Type) {
		respond(s, i, "テキスト、ニュース、フォーラムチャンネルだけ登録できます。", true)
		return
	}
	if groupID == "" {
		groupID = ch.Name
	}
	groupID = strings.TrimSpace(groupID)
	webhookID, token, err := h.api.CreateWebhook(channelID, "Gemini Auto Translator")
	if err != nil {
		respond(s, i, "Webhookを作成できませんでした: "+err.Error(), true)
		return
	}
	err = h.store.CreateGroupWithChannel(ctx, TranslationGroup{
		ID: groupID, GuildID: i.GuildID, DisplayName: groupID, CreatedBy: i.Member.User.ID,
	}, GroupChannel{
		GroupID: groupID, GuildID: i.GuildID, ChannelID: channelID, ChannelType: int(ch.Type), Language: language, WebhookID: webhookID, WebhookToken: token,
	})
	if err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("翻訳グループ `%s` に <#%s> (%s) を登録しました。", groupID, channelID, language), true)
}

func (h *CommandHandler) handleJoinChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := optionString(data.Options, "group")
	language := normalizeLanguage(optionString(data.Options, "language"))
	if !IsValidLanguageCode(language) {
		respond(s, i, "言語は `en`, `ja`, `zh-CN`, `pt-BR` のようなBCP-47形式の短いコードで指定してください。", true)
		return
	}
	channelID := optionChannel(data.Options, "channel", i.ChannelID)
	ch, err := s.Channel(channelID)
	if err != nil {
		respond(s, i, "チャンネルを取得できませんでした: "+err.Error(), true)
		return
	}
	if !allowedChannelType(ch.Type) {
		respond(s, i, "テキスト、ニュース、フォーラムチャンネルだけ参加できます。", true)
		return
	}
	webhookID, token, err := h.api.CreateWebhook(channelID, "Gemini Auto Translator")
	if err != nil {
		respond(s, i, "Webhookを作成できませんでした: "+err.Error(), true)
		return
	}
	err = h.store.JoinChannel(ctx, GroupChannel{
		GroupID: groupID, GuildID: i.GuildID, ChannelID: channelID, ChannelType: int(ch.Type), Language: language, WebhookID: webhookID, WebhookToken: token,
	})
	if err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("翻訳グループ `%s` に <#%s> (%s) を参加させました。", groupID, channelID, language), true)
}

func (h *CommandHandler) handleLeaveChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := optionString(data.Options, "group")
	channelID := optionChannel(data.Options, "channel", i.ChannelID)
	if err := h.store.LeaveChannel(ctx, i.GuildID, groupID, channelID); err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("翻訳グループ `%s` から <#%s> を退出させました。", groupID, channelID), true)
}

func (h *CommandHandler) handleDeleteGroup(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := optionString(data.Options, "group")
	if err := h.store.DeleteGroup(ctx, i.GuildID, groupID); err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("翻訳グループ `%s` を削除しました。", groupID), true)
}

func RegisterGuildCommands(s *discordgo.Session, appID string) {
	for _, g := range s.State.Guilds {
		for _, cmd := range Commands() {
			if _, err := s.ApplicationCommandCreate(appID, g.ID, cmd); err != nil {
				log.Printf("register command %s in guild %s: %v", cmd.Name, g.ID, err)
			}
		}
	}
}

func focusedOption(options []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
	for _, o := range options {
		if o.Focused {
			return o
		}
	}
	return nil
}

func optionString(options []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, o := range options {
		if o.Name == name {
			return o.StringValue()
		}
	}
	return ""
}

func optionChannel(options []*discordgo.ApplicationCommandInteractionDataOption, name, fallback string) string {
	for _, o := range options {
		if o.Name == name {
			if channelID, ok := o.Value.(string); ok && channelID != "" {
				return channelID
			}
			return o.ChannelValue(nil).ID
		}
	}
	return fallback
}

func allowedChannelType(t discordgo.ChannelType) bool {
	return t == discordgo.ChannelTypeGuildText || t == discordgo.ChannelTypeGuildNews || t == discordgo.ChannelTypeGuildForum || t == discordgo.ChannelTypeGuildMedia
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, msg string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Flags: flags},
	})
}
