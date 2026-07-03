package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var defaultAdminCommandPermissions int64 = discordgo.PermissionAdministrator

const viewOriginalCommandName = "View Original"

func Commands() []*discordgo.ApplicationCommand {
	channelTypes := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildForum,
		discordgo.ChannelTypeGuildMedia,
	}
	cmds := []*discordgo.ApplicationCommand{
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
				{Name: "attribute", Description: "Term type, such as person name, slang, or a custom value", Type: discordgo.ApplicationCommandOptionString, Required: false, Autocomplete: true},
				{Name: "always_include", Description: "Always include this term in translation instructions", Type: discordgo.ApplicationCommandOptionBoolean, Required: false},
			},
		},
		{
			Name:        "list-groups",
			Description: "List translation groups and channels for this server",
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
		{
			Name:        "set-style",
			Description: "Set translation style for a group (preset or custom instruction)",
			Options: []*discordgo.ApplicationCommandOption{
				{Name: "group", Description: "Existing translation group", Type: discordgo.ApplicationCommandOptionString, Required: true, Autocomplete: true},
				{Name: "preset", Description: "Style preset", Type: discordgo.ApplicationCommandOptionString, Required: false, Choices: StylePresetChoices()},
				{Name: "custom", Description: "Custom style instruction in natural language", Type: discordgo.ApplicationCommandOptionString, Required: false},
			},
		},
	}
	for _, cmd := range cmds {
		cmd.DefaultMemberPermissions = &defaultAdminCommandPermissions
	}
	cmds = append(cmds, &discordgo.ApplicationCommand{
		Name: viewOriginalCommandName,
		Type: discordgo.MessageApplicationCommand,
	})
	return cmds
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
	if i.Type == discordgo.InteractionApplicationCommand && data.CommandType == discordgo.MessageApplicationCommand {
		if data.Name == viewOriginalCommandName {
			h.handleViewOriginal(s, i, data)
		}
		return
	}
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
	case "list-groups":
		h.handleListGroups(s, i, data)
	case "list-glossary":
		h.handleListGlossary(s, i, data)
	case "remove-glossary":
		h.handleRemoveGlossary(s, i, data)
	case "set-style":
		h.handleSetStyle(s, i, data)
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
	if focused != nil && focused.Name == "attribute" {
		for _, attribute := range glossaryAttributeSuggestions(focused.StringValue(), 25) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: attribute, Value: attribute})
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
	attribute := optionString(data.Options, "attribute")
	alwaysInclude := optionBool(data.Options, "always_include")
	if err := h.store.UpsertGlossaryEntry(ctx, i.GuildID, term, translation, attribute, i.Member.User.ID, alwaysInclude); err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	mode := "本文に含まれる場合のみ使用"
	if alwaysInclude {
		mode = "常に使用"
	}
	attributeLabel := "属性なし"
	if strings.TrimSpace(attribute) != "" {
		attributeLabel = "属性: " + strings.TrimSpace(attribute)
	}
	respond(s, i, fmt.Sprintf("用語 `%s` を `%s` として登録しました（%s、%s）。", strings.TrimSpace(term), strings.TrimSpace(translation), attributeLabel, mode), true)
}

const (
	discordMessageMaxLen        = 2000
	listGroupsTruncatedSuffix   = "\n（表示を省略しました）"
	listGlossaryTruncatedSuffix = "\n（残りの用語は表示を省略しました）"
)

func (h *CommandHandler) handleListGroups(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groups, err := h.store.Groups(ctx, i.GuildID, "", 100)
	if err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	if len(groups) == 0 {
		respond(s, i, "このサーバーには翻訳グループが登録されていません。", true)
		return
	}
	msg, err := formatListGroupsMessage(ctx, h.store, i.GuildID, groups)
	if err != nil {
		respond(s, i, err.Error(), true)
		return
	}
	respond(s, i, msg, true)
}

func formatListGroupsMessage(ctx context.Context, store *Store, guildID string, groups []TranslationGroup) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "翻訳グループ (%d):\n", len(groups))
	truncated := false
	for _, g := range groups {
		channels, err := store.ChannelsInGroup(ctx, guildID, g.ID)
		if err != nil {
			return "", err
		}
		groupBlock := formatGroupBlock(g, channels)
		if b.Len()+len(groupBlock)+len(listGroupsTruncatedSuffix) > discordMessageMaxLen {
			truncated = true
			break
		}
		b.WriteString(groupBlock)
	}
	result := strings.TrimSpace(b.String())
	if truncated {
		result += listGroupsTruncatedSuffix
	}
	return result, nil
}

func formatGroupBlock(g TranslationGroup, channels []GroupChannel) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n**%s** (style: %s)\n", g.DisplayName, FormatGroupStyle(g))
	if len(channels) == 0 {
		b.WriteString("  （チャンネルなし）\n")
		return b.String()
	}
	for _, ch := range channels {
		fmt.Fprintf(&b, "  - <#%s> (%s)\n", ch.ChannelID, ch.Language)
	}
	return b.String()
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
	fmt.Fprintf(&b, "グロッサリー (%d):\n", len(entries))
	truncated := false
	for _, entry := range entries {
		mode := "一致時"
		if entry.AlwaysInclude {
			mode = "常時"
		}
		attribute := "属性なし"
		if entry.Attribute != "" {
			attribute = entry.Attribute
		}
		line := fmt.Sprintf("- `%s` → `%s`（%s、%s）\n", entry.SourceTerm, entry.PreferredTranslation, attribute, mode)
		if b.Len()+len(line)+len(listGlossaryTruncatedSuffix) > discordMessageMaxLen {
			truncated = true
			break
		}
		b.WriteString(line)
	}
	msg := strings.TrimSpace(b.String())
	if truncated {
		msg += listGlossaryTruncatedSuffix
	}
	respond(s, i, msg, true)
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

func (h *CommandHandler) handleSetStyle(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if groupID == "" {
		respond(s, i, "グループ名を指定してください。", true)
		return
	}
	preset := strings.TrimSpace(optionString(data.Options, "preset"))
	custom := strings.TrimSpace(optionString(data.Options, "custom"))

	if preset != "" && custom != "" {
		respond(s, i, "プリセットとカスタム指示は同時に指定できません。どちらか一方だけ指定してください。", true)
		return
	}
	if preset == "" && custom == "" {
		respond(s, i, "プリセットまたはカスタム指示のどちらかを指定してください。スタイルをリセットする場合は `preset:default` を指定してください。", true)
		return
	}

	var storePreset, storeCustom string
	switch {
	case preset == StylePresetDefault:
		storePreset, storeCustom = "", ""
	case preset != "":
		if !IsValidStylePreset(preset) {
			respond(s, i, "不明なプリセットです。コマンドの選択肢から指定してください。", true)
			return
		}
		storePreset, storeCustom = preset, ""
	default:
		if err := ValidateStyleCustom(custom); err != nil {
			respond(s, i, err.Error(), true)
			return
		}
		storePreset, storeCustom = "", custom
	}

	if err := h.store.SetGroupStyle(ctx, i.GuildID, groupID, storePreset, storeCustom); err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			respond(s, i, fmt.Sprintf("翻訳グループ `%s` がこのサーバーに見つかりません。", groupID), true)
			return
		}
		respond(s, i, err.Error(), true)
		return
	}

	switch {
	case storeCustom != "":
		respond(s, i, fmt.Sprintf("翻訳グループ `%s` のスタイルをカスタム指示に設定しました: `%s`", groupID, storeCustom), true)
	case storePreset != "":
		respond(s, i, fmt.Sprintf("翻訳グループ `%s` のスタイルをプリセット `%s` に設定しました。", groupID, storePreset), true)
	default:
		respond(s, i, fmt.Sprintf("翻訳グループ `%s` のスタイルをリセットしました。", groupID), true)
	}
}

func (h *CommandHandler) handleViewOriginal(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	channelID := i.ChannelID
	messageID := data.TargetID
	guildID := i.GuildID

	result, ok, err := h.store.MessageOriginal(ctx, channelID, messageID)
	if err != nil {
		log.Printf("view-original lookup: %v", err)
		respond(s, i, localizedUIString("en", uiKeyNotManaged), true)
		return
	}
	lang := h.uiLanguageForChannel(ctx, guildID, channelID, result)
	if !ok {
		respond(s, i, localizedUIString(lang, uiKeyNotManaged), true)
		return
	}
	if result.IsSource {
		respond(s, i, localizedUIString(lang, uiKeyAlreadyOriginal), true)
		return
	}

	url := messageJumpURL(guildID, result.SourceChannelID, result.SourceMessageID)
	linkLabel := localizedUIString(lang, uiKeyViewOriginalLink)
	msg := fmt.Sprintf("[%s](%s)", linkLabel, url)
	if snippet := truncateSnapshot(result.Snapshot, 100); snippet != "" {
		msg += "\n\n> " + snippet
	}
	respond(s, i, msg, true)
}

func (h *CommandHandler) uiLanguageForChannel(ctx context.Context, guildID, channelID string, result MessageOriginalResult) string {
	if result.TargetLanguage != "" {
		return resolveUILanguage(result.TargetLanguage)
	}
	channels, err := h.store.ChannelsByChannel(ctx, guildID, channelID)
	if err == nil && len(channels) > 0 {
		return resolveUILanguage(channels[0].Language)
	}
	threads, err := h.store.ThreadTargets(ctx, channelID)
	if err == nil && len(threads) > 0 {
		if threads[0].TargetLanguage != "" {
			return resolveUILanguage(threads[0].TargetLanguage)
		}
		for _, parentID := range []string{threads[0].TargetChannelID, threads[0].SourceChannelID} {
			if parentID == "" {
				continue
			}
			channels, err = h.store.ChannelsByChannel(ctx, guildID, parentID)
			if err == nil && len(channels) > 0 {
				return resolveUILanguage(channels[0].Language)
			}
		}
	}
	return "en"
}

func messageJumpURL(guildID, channelID, messageID string) string {
	return MessageJumpURL(guildID, channelID, messageID)
}

func truncateSnapshot(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}

func RegisterGuildCommands(s *discordgo.Session, appID string) {
	for _, g := range s.State.Guilds {
		if err := RegisterGuildCommandsForGuild(s, appID, g.ID); err != nil {
			log.Printf("register commands in guild %s: %v", g.ID, err)
		}
	}
}

func RegisterGuildCommandsForGuild(s *discordgo.Session, appID, guildID string) error {
	_, err := s.ApplicationCommandBulkOverwrite(appID, guildID, Commands())
	return err
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

func optionBool(options []*discordgo.ApplicationCommandInteractionDataOption, name string) bool {
	for _, o := range options {
		if o.Name == name {
			value, _ := o.Value.(bool)
			return value
		}
	}
	return false
}

var glossaryAttributeDefaults = []string{"人名", "地名", "スラング", "略語", "専門用語"}

func glossaryAttributeSuggestions(query string, limit int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]string, 0, len(glossaryAttributeDefaults))
	for _, attribute := range glossaryAttributeDefaults {
		if query == "" || strings.Contains(strings.ToLower(attribute), query) {
			result = append(result, attribute)
			if len(result) == limit {
				break
			}
		}
	}
	return result
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

var interactionResponseHook func(msg string, ephemeral bool)

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, msg string, ephemeral bool) {
	if interactionResponseHook != nil {
		interactionResponseHook(msg, ephemeral)
		return
	}
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Flags: flags},
	})
}
