package translatorbot

import (
	"context"
	"errors"
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
		{
			Name:        "add-glossary",
			Description: "Register a preferred translation for a term in this server",
			Options: []*discordgo.ApplicationCommandOption{
				{Name: "term", Description: "Source term to match", Type: discordgo.ApplicationCommandOptionString, Required: true},
				{Name: "translation", Description: "Preferred translation", Type: discordgo.ApplicationCommandOptionString, Required: true},
			},
		},
		{
			Name:        "list-glossary",
			Description: "List glossary entries for this server",
		},
		{
			Name:        "remove-glossary",
			Description: "Remove a glossary entry from this server",
			Options: []*discordgo.ApplicationCommandOption{
				{Name: "term", Description: "Source term to remove", Type: discordgo.ApplicationCommandOptionString, Required: true},
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
	case "add-glossary":
		h.handleAddGlossary(s, i, data)
	case "list-glossary":
		h.handleListGlossary(s, i, data)
	case "remove-glossary":
		h.handleRemoveGlossary(s, i, data)
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
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if groupID == "" {
		respond(s, i, "グループ名を指定してください。", true)
		return
	}
	language := normalizeLanguage(optionString(data.Options, "language"))
	if !IsValidLanguageCode(language) {
		respond(s, i, "言語は `en`, `ja`, `zh-CN`, `pt-BR` のようなBCP-47形式の短いコードで指定してください。", true)
		return
	}
	exists, err := h.store.GroupExists(ctx, i.GuildID, groupID)
	if err != nil {
		log.Printf("join-channel group exists check: %v", err)
		respond(s, i, "チャンネルを参加させられませんでした。", true)
		return
	}
	if !exists {
		respond(s, i, joinChannelErrorMessage(groupID, ErrGroupNotFound), true)
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
		log.Printf("join-channel store: %v", err)
		respond(s, i, joinChannelErrorMessage(groupID, err), true)
		return
	}
	respond(s, i, fmt.Sprintf("翻訳グループ `%s` に <#%s> (%s) を参加させました。", groupID, channelID, language), true)
}

func (h *CommandHandler) handleLeaveChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	channelID := optionChannel(data.Options, "channel", i.ChannelID)
	if err := h.store.LeaveChannel(ctx, i.GuildID, groupID, channelID); err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("翻訳グループ `%s` から <#%s> を退出させました。", groupID, channelID), true)
}

func (h *CommandHandler) handleDeleteGroup(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if err := h.store.DeleteGroup(ctx, i.GuildID, groupID); err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("翻訳グループ `%s` を削除しました。", groupID), true)
}

func (h *CommandHandler) handleAddGlossary(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	term := optionString(data.Options, "term")
	translation := optionString(data.Options, "translation")
	if err := h.store.UpsertGlossaryEntry(ctx, i.GuildID, term, translation, i.Member.User.ID); err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("用語 `%s` を `%s` として登録しました。", strings.TrimSpace(term), strings.TrimSpace(translation)), true)
}

func (h *CommandHandler) handleListGlossary(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	entries, err := h.store.ListGlossaryEntries(ctx, i.GuildID)
	if err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	if len(entries) == 0 {
		respond(s, i, "このサーバーにはグロッサリー登録がありません。", true)
		return
	}
	var b strings.Builder
	b.WriteString("グロッサリー:\n")
	for _, entry := range entries {
		fmt.Fprintf(&b, "- `%s` → `%s`\n", entry.SourceTerm, entry.PreferredTranslation)
	}
	respond(s, i, strings.TrimSpace(b.String()), true)
}

func (h *CommandHandler) handleRemoveGlossary(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	term := optionString(data.Options, "term")
	if err := h.store.RemoveGlossaryEntry(ctx, i.GuildID, term); err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, fmt.Sprintf("用語 `%s` を削除しました。", strings.TrimSpace(term)), true)
}

func RegisterGuildCommands(s *discordgo.Session, appID string) map[string]string {
	addGlossaryCommandIDs := make(map[string]string)
	for _, g := range s.State.Guilds {
		for _, cmd := range Commands() {
			created, err := s.ApplicationCommandCreate(appID, g.ID, cmd)
			if err != nil {
				log.Printf("register command %s in guild %s: %v", cmd.Name, g.ID, err)
				continue
			}
			if cmd.Name == "add-glossary" && created != nil {
				addGlossaryCommandIDs[g.ID] = created.ID
			}
		}
	}
	return addGlossaryCommandIDs
}

func joinChannelErrorMessage(groupID string, err error) string {
	switch {
	case errors.Is(err, ErrGroupNotFound):
		return fmt.Sprintf("翻訳グループ `%s` がこのサーバーに見つかりません。`/new-channel` で作成したグループ名と一致しているか確認してください。", groupID)
	case errors.Is(err, ErrDuplicateChannel):
		return fmt.Sprintf("このチャンネルは既にグループ `%s` に参加しています。", groupID)
	case errors.Is(err, ErrDuplicateLanguage):
		return fmt.Sprintf("グループ `%s` には既に同じ言語のチャンネルがあります。別の言語を指定してください。", groupID)
	default:
		return "チャンネルを参加させられませんでした。"
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
