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
const botWhitelistCommandName = "bot-whitelist"

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
		{
			Name:        botWhitelistCommandName,
			Description: "Manage allowed bot and webhook sources for this server",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "add",
					Description: "Allow a bot or webhook source in this server",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Name: "source_type", Description: "Automated message source type", Type: discordgo.ApplicationCommandOptionString, Required: true, Choices: sourceTypeChoices()},
						{Name: "source_id", Description: "Discord bot user ID or webhook ID", Type: discordgo.ApplicationCommandOptionString, Required: true},
					},
				},
				{
					Name:        "remove",
					Description: "Remove a bot or webhook source from this server",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Name: "source_type", Description: "Automated message source type", Type: discordgo.ApplicationCommandOptionString, Required: true, Choices: sourceTypeChoices()},
						{Name: "source_id", Description: "Discord bot user ID or webhook ID", Type: discordgo.ApplicationCommandOptionString, Required: true},
					},
				},
				{
					Name:        "list",
					Description: "List allowed bot and webhook sources for this server",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
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
	// respond delivers the interaction response; replaced in tests.
	respond func(s *discordgo.Session, i *discordgo.InteractionCreate, msg string, ephemeral bool)
}

func NewCommandHandler(store *Store, api DiscordAPI) *CommandHandler {
	return &CommandHandler{store: store, api: api, respond: respondInteraction}
}

// commandLocale resolves the invoking user's Discord client locale to a
// supported catalog language for ephemeral command responses.
func commandLocale(i *discordgo.InteractionCreate) string {
	return resolveUILanguage(string(i.Locale))
}

// reply sends a localized catalog message as the interaction response.
func (h *CommandHandler) reply(s *discordgo.Session, i *discordgo.InteractionCreate, key uiKey, args ...any) {
	h.respond(s, i, localizedUIStringf(commandLocale(i), key, args...), true)
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
	if data.Name == botWhitelistCommandName && i.GuildID == "" {
		h.reply(s, i, uiKeyGuildOnly)
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
	case botWhitelistCommandName:
		h.handleBotWhitelist(s, i, data)
	}
}

func sourceTypeChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Bot", Value: string(SourceTypeBot)},
		{Name: "Webhook", Value: string(SourceTypeWebhook)},
	}
}

func (h *CommandHandler) handleBotWhitelist(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if len(data.Options) != 1 || data.Options[0].Type != discordgo.ApplicationCommandOptionSubCommand {
		log.Printf("%s invalid subcommand payload", botWhitelistCommandName)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	subcommand := data.Options[0]
	switch subcommand.Name {
	case "add":
		h.handleAddSource(s, i, subcommand.Options)
	case "remove":
		h.handleRemoveSource(s, i, subcommand.Options)
	case "list":
		h.handleListSources(s, i)
	default:
		log.Printf("%s unknown subcommand %q", botWhitelistCommandName, subcommand.Name)
		h.reply(s, i, uiKeyUnexpectedError)
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

// resolveCommandChannel fetches the target channel of a command and verifies
// its type is supported, replying with a localized error otherwise.
func (h *CommandHandler) resolveCommandChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) (*discordgo.Channel, bool) {
	channelID := optionChannel(data.Options, "channel", i.ChannelID)
	ch, err := s.Channel(channelID)
	if err != nil {
		log.Printf("%s fetch channel %s: %v", data.Name, channelID, err)
		h.reply(s, i, uiKeyChannelFetchFailed)
		return nil, false
	}
	if !allowedChannelType(ch.Type) {
		h.reply(s, i, uiKeyUnsupportedChannelType)
		return nil, false
	}
	return ch, true
}

func (h *CommandHandler) handleNewChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	language := normalizeLanguage(optionString(data.Options, "language"))
	if !IsValidLanguageCode(language) {
		h.reply(s, i, uiKeyInvalidLanguage)
		return
	}
	ch, ok := h.resolveCommandChannel(s, i, data)
	if !ok {
		return
	}
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if groupID == "" {
		groupID = ch.Name
	}
	webhookID, token, err := h.api.CreateWebhook(ch.ID, defaultWebhookName)
	if err != nil {
		log.Printf("new-channel create webhook: %v", err)
		h.reply(s, i, uiKeyWebhookCreateFailed)
		return
	}
	err = h.store.CreateGroupWithChannel(ctx, TranslationGroup{
		ID: groupID, GuildID: i.GuildID, DisplayName: groupID, CreatedBy: i.Member.User.ID,
	}, GroupChannel{
		GroupID: groupID, GuildID: i.GuildID, ChannelID: ch.ID, ChannelType: int(ch.Type), Language: language, WebhookID: webhookID, WebhookToken: token,
	})
	if err != nil {
		h.replyGroupError(s, i, "new-channel", groupID, err)
		return
	}
	h.reply(s, i, uiKeyChannelRegistered, groupID, ch.ID, language)
}

func (h *CommandHandler) handleJoinChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if groupID == "" {
		h.reply(s, i, uiKeyGroupRequired)
		return
	}
	language := normalizeLanguage(optionString(data.Options, "language"))
	if !IsValidLanguageCode(language) {
		h.reply(s, i, uiKeyInvalidLanguage)
		return
	}
	// The group is checked before creating the webhook so a typo does not
	// leave an orphaned webhook behind.
	exists, err := h.store.GroupExists(ctx, i.GuildID, groupID)
	if err != nil {
		log.Printf("join-channel group exists check: %v", err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	if !exists {
		h.reply(s, i, uiKeyJoinGroupNotFound, groupID)
		return
	}
	ch, ok := h.resolveCommandChannel(s, i, data)
	if !ok {
		return
	}
	webhookID, token, err := h.api.CreateWebhook(ch.ID, defaultWebhookName)
	if err != nil {
		log.Printf("join-channel create webhook: %v", err)
		h.reply(s, i, uiKeyWebhookCreateFailed)
		return
	}
	err = h.store.JoinChannel(ctx, GroupChannel{
		GroupID: groupID, GuildID: i.GuildID, ChannelID: ch.ID, ChannelType: int(ch.Type), Language: language, WebhookID: webhookID, WebhookToken: token,
	})
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			h.reply(s, i, uiKeyJoinGroupNotFound, groupID)
			return
		}
		h.replyGroupError(s, i, "join-channel", groupID, err)
		return
	}
	h.reply(s, i, uiKeyChannelJoined, groupID, ch.ID, language)
}

func (h *CommandHandler) handleLeaveChannel(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	channelID := optionChannel(data.Options, "channel", i.ChannelID)
	if err := h.store.LeaveChannel(ctx, i.GuildID, groupID, channelID); err != nil {
		h.replyGroupError(s, i, "leave-channel", groupID, err)
		return
	}
	h.reply(s, i, uiKeyChannelLeft, groupID, channelID)
}

func (h *CommandHandler) handleDeleteGroup(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if err := h.store.DeleteGroup(ctx, i.GuildID, groupID); err != nil {
		h.replyGroupError(s, i, "delete-group", groupID, err)
		return
	}
	h.reply(s, i, uiKeyGroupDeleted, groupID)
}

// replyGroupError maps store errors about groups and channels to localized
// messages; unexpected errors are logged and reported generically.
func (h *CommandHandler) replyGroupError(s *discordgo.Session, i *discordgo.InteractionCreate, command, groupID string, err error) {
	switch {
	case errors.Is(err, ErrGroupNotFound):
		h.reply(s, i, uiKeyGroupNotFound, groupID)
	case errors.Is(err, ErrDuplicateGroup):
		h.reply(s, i, uiKeyDuplicateGroup, groupID)
	case errors.Is(err, ErrDuplicateChannel):
		h.reply(s, i, uiKeyDuplicateChannel, groupID)
	case errors.Is(err, ErrDuplicateLanguage):
		h.reply(s, i, uiKeyDuplicateLanguage, groupID)
	case errors.Is(err, ErrChannelNotFound):
		h.reply(s, i, uiKeyChannelNotJoined, groupID)
	default:
		log.Printf("%s store: %v", command, err)
		h.reply(s, i, uiKeyUnexpectedError)
	}
}

func (h *CommandHandler) handleAddGlossary(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	lang := commandLocale(i)
	term := optionString(data.Options, "term")
	translation := optionString(data.Options, "translation")
	attribute := optionString(data.Options, "attribute")
	alwaysInclude := optionBool(data.Options, "always_include")
	if err := h.store.UpsertGlossaryEntry(ctx, i.GuildID, term, translation, attribute, i.Member.User.ID, alwaysInclude); err != nil {
		switch {
		case errors.Is(err, ErrGlossaryTermRequired):
			h.reply(s, i, uiKeyGlossaryTermRequired)
		case errors.Is(err, ErrGlossaryFull):
			h.reply(s, i, uiKeyGlossaryFull, glossaryMaxEntries)
		default:
			log.Printf("add-glossary store: %v", err)
			h.reply(s, i, uiKeyUnexpectedError)
		}
		return
	}
	mode := localizedUIString(lang, uiKeyGlossaryModeMatched)
	if alwaysInclude {
		mode = localizedUIString(lang, uiKeyGlossaryModeAlways)
	}
	attributeLabel := localizedUIString(lang, uiKeyGlossaryAttributeNone)
	if strings.TrimSpace(attribute) != "" {
		attributeLabel = localizedUIStringf(lang, uiKeyGlossaryAttributeLabel, strings.TrimSpace(attribute))
	}
	h.reply(s, i, uiKeyGlossaryAdded, strings.TrimSpace(term), strings.TrimSpace(translation), attributeLabel, mode)
}

func (h *CommandHandler) handleListGroups(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groups, err := h.store.Groups(ctx, i.GuildID, "", 100)
	if err != nil {
		log.Printf("list-groups store: %v", err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	if len(groups) == 0 {
		h.reply(s, i, uiKeyNoGroups)
		return
	}
	msg, err := formatListGroupsMessage(ctx, h.store, i.GuildID, commandLocale(i), groups)
	if err != nil {
		log.Printf("list-groups store: %v", err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	h.respond(s, i, msg, true)
}

func formatListGroupsMessage(ctx context.Context, store *Store, guildID, lang string, groups []TranslationGroup) (string, error) {
	truncatedSuffix := localizedUIString(lang, uiKeyGroupsTruncated)
	var b strings.Builder
	b.WriteString(localizedUIStringf(lang, uiKeyGroupsHeader, len(groups)))
	b.WriteString("\n")
	truncated := false
	for _, g := range groups {
		channels, err := store.ChannelsInGroup(ctx, guildID, g.ID)
		if err != nil {
			return "", err
		}
		groupBlock := formatGroupBlock(g, channels, lang)
		if b.Len()+len(groupBlock)+len(truncatedSuffix) > discordMessageContentLimit {
			truncated = true
			break
		}
		b.WriteString(groupBlock)
	}
	result := strings.TrimSpace(b.String())
	if truncated {
		result += truncatedSuffix
	}
	return result, nil
}

func formatGroupBlock(g TranslationGroup, channels []GroupChannel, lang string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n**%s** (style: %s)\n", g.DisplayName, FormatGroupStyle(g))
	if len(channels) == 0 {
		b.WriteString(localizedUIString(lang, uiKeyGroupNoChannels))
		b.WriteString("\n")
		return b.String()
	}
	for _, ch := range channels {
		fmt.Fprintf(&b, "  - <#%s> (%s)\n", ch.ChannelID, ch.Language)
	}
	return b.String()
}

func (h *CommandHandler) handleListGlossary(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	lang := commandLocale(i)
	entries, err := h.store.ListGlossaryEntries(ctx, i.GuildID)
	if err != nil {
		log.Printf("list-glossary store: %v", err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	if len(entries) == 0 {
		h.reply(s, i, uiKeyNoGlossary)
		return
	}
	truncatedSuffix := localizedUIString(lang, uiKeyGlossaryTruncated)
	var b strings.Builder
	b.WriteString(localizedUIStringf(lang, uiKeyGlossaryHeader, len(entries)))
	b.WriteString("\n")
	truncated := false
	for _, entry := range entries {
		mode := localizedUIString(lang, uiKeyGlossaryModeMatched)
		if entry.AlwaysInclude {
			mode = localizedUIString(lang, uiKeyGlossaryModeAlways)
		}
		attribute := localizedUIString(lang, uiKeyGlossaryAttributeNone)
		if entry.Attribute != "" {
			attribute = entry.Attribute
		}
		line := fmt.Sprintf("- `%s` → `%s` (%s, %s)\n", entry.SourceTerm, entry.PreferredTranslation, attribute, mode)
		if b.Len()+len(line)+len(truncatedSuffix) > discordMessageContentLimit {
			truncated = true
			break
		}
		b.WriteString(line)
	}
	msg := strings.TrimSpace(b.String())
	if truncated {
		msg += truncatedSuffix
	}
	h.respond(s, i, msg, true)
}

func (h *CommandHandler) handleRemoveGlossary(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	term := strings.TrimSpace(optionString(data.Options, "term"))
	if err := h.store.RemoveGlossaryEntry(ctx, i.GuildID, term); err != nil {
		switch {
		case errors.Is(err, ErrGlossaryNotFound), errors.Is(err, ErrGlossaryTermRequired):
			h.reply(s, i, uiKeyGlossaryNotFound, term)
		default:
			log.Printf("remove-glossary store: %v", err)
			h.reply(s, i, uiKeyUnexpectedError)
		}
		return
	}
	h.reply(s, i, uiKeyGlossaryRemoved, term)
}

func (h *CommandHandler) handleSetStyle(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if groupID == "" {
		h.reply(s, i, uiKeyGroupRequired)
		return
	}
	preset := strings.TrimSpace(optionString(data.Options, "preset"))
	custom := strings.TrimSpace(optionString(data.Options, "custom"))

	if preset != "" && custom != "" {
		h.reply(s, i, uiKeyStyleBothSpecified)
		return
	}
	if preset == "" && custom == "" {
		h.reply(s, i, uiKeyStyleNoneSpecified)
		return
	}

	var storePreset, storeCustom string
	switch {
	case preset == StylePresetDefault:
		storePreset, storeCustom = "", ""
	case preset != "":
		if !IsValidStylePreset(preset) {
			h.reply(s, i, uiKeyStyleUnknownPreset)
			return
		}
		storePreset, storeCustom = preset, ""
	default:
		if err := ValidateStyleCustom(custom); err != nil {
			switch {
			case errors.Is(err, ErrStyleCustomEmpty):
				h.reply(s, i, uiKeyStyleCustomEmpty)
			case errors.Is(err, ErrStyleCustomTooLong):
				h.reply(s, i, uiKeyStyleCustomTooLong, styleCustomMaxRunes)
			default:
				log.Printf("set-style validate: %v", err)
				h.reply(s, i, uiKeyUnexpectedError)
			}
			return
		}
		storePreset, storeCustom = "", custom
	}

	if err := h.store.SetGroupStyle(ctx, i.GuildID, groupID, storePreset, storeCustom); err != nil {
		h.replyGroupError(s, i, "set-style", groupID, err)
		return
	}

	switch {
	case storeCustom != "":
		h.reply(s, i, uiKeyStyleCustomSet, groupID, storeCustom)
	case storePreset != "":
		h.reply(s, i, uiKeyStylePresetSet, groupID, storePreset)
	default:
		h.reply(s, i, uiKeyStyleReset, groupID)
	}
}

func (h *CommandHandler) handleAddSource(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	sourceType, sourceID, ok := h.sourceOptions(s, i, options)
	if !ok {
		return
	}
	createdBy := ""
	if i.Member != nil && i.Member.User != nil {
		createdBy = i.Member.User.ID
	}
	if err := h.store.AddAllowedSource(context.Background(), i.GuildID, sourceType, sourceID, createdBy); err != nil {
		switch {
		case errors.Is(err, ErrSourceAlreadyAllowed):
			h.reply(s, i, uiKeySourceAlreadyAllowed, sourceType, sourceID)
		case errors.Is(err, ErrManagedWebhook):
			h.reply(s, i, uiKeyManagedWebhookRejected)
		default:
			log.Printf("%s add store: %v", botWhitelistCommandName, err)
			h.reply(s, i, uiKeyUnexpectedError)
		}
		return
	}
	h.reply(s, i, uiKeySourceAllowed, sourceType, sourceID)
}

func (h *CommandHandler) handleRemoveSource(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	sourceType, sourceID, ok := h.sourceOptions(s, i, options)
	if !ok {
		return
	}
	if err := h.store.RemoveAllowedSource(context.Background(), i.GuildID, sourceType, sourceID); err != nil {
		if errors.Is(err, ErrSourceNotAllowed) {
			h.reply(s, i, uiKeySourceNotAllowed, sourceType, sourceID)
		} else {
			log.Printf("%s remove store: %v", botWhitelistCommandName, err)
			h.reply(s, i, uiKeyUnexpectedError)
		}
		return
	}
	h.reply(s, i, uiKeySourceRemoved, sourceType, sourceID)
}

func (h *CommandHandler) sourceOptions(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) (SourceType, string, bool) {
	sourceType, err := ParseSourceType(optionString(options, "source_type"))
	if err != nil {
		h.reply(s, i, uiKeyInvalidSourceType)
		return "", "", false
	}
	sourceID := strings.TrimSpace(optionString(options, "source_id"))
	if err := ValidateCanonicalSnowflake(sourceID); err != nil {
		h.reply(s, i, uiKeyInvalidSourceID)
		return "", "", false
	}
	return sourceType, sourceID, true
}

func (h *CommandHandler) handleListSources(s *discordgo.Session, i *discordgo.InteractionCreate) {
	sources, err := h.store.ListAllowedSources(context.Background(), i.GuildID)
	if err != nil {
		log.Printf("%s list store: %v", botWhitelistCommandName, err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	if len(sources) == 0 {
		h.reply(s, i, uiKeyNoAllowedSources)
		return
	}
	lang := commandLocale(i)
	truncatedSuffix := localizedUIString(lang, uiKeyAllowedSourcesTruncated)
	var b strings.Builder
	b.WriteString(localizedUIStringf(lang, uiKeyAllowedSourcesHeader, len(sources)))
	b.WriteString("\n")
	truncated := false
	for _, source := range sources {
		line := fmt.Sprintf("- `%s`: `%s`\n", source.Type, source.ID)
		if b.Len()+len(line)+len(truncatedSuffix) > discordMessageContentLimit {
			truncated = true
			break
		}
		b.WriteString(line)
	}
	msg := strings.TrimSpace(b.String())
	if truncated {
		msg += truncatedSuffix
	}
	h.respond(s, i, msg, true)
}

func (h *CommandHandler) handleViewOriginal(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	channelID := i.ChannelID
	messageID := data.TargetID
	guildID := i.GuildID

	result, ok, err := h.store.MessageOriginal(ctx, channelID, messageID)
	if err != nil {
		log.Printf("view-original lookup: %v", err)
		h.respond(s, i, localizedUIString(commandLocale(i), uiKeyNotManaged), true)
		return
	}
	// View Original follows the channel's registered language so the reply
	// matches the language of the surrounding conversation.
	lang := h.uiLanguageForChannel(ctx, guildID, channelID, result)
	if !ok {
		h.respond(s, i, localizedUIString(lang, uiKeyNotManaged), true)
		return
	}
	if result.IsSource {
		h.respond(s, i, localizedUIString(lang, uiKeyAlreadyOriginal), true)
		return
	}

	url := MessageJumpURL(guildID, result.SourceChannelID, result.SourceMessageID)
	linkLabel := localizedUIString(lang, uiKeyViewOriginalLink)
	msg := fmt.Sprintf("[%s](%s)", linkLabel, url)
	if snippet := strings.TrimSpace(result.Snapshot); snippet != "" {
		msg += "\n\n> " + truncateRunes(snippet, 100, "…")
	}
	h.respond(s, i, msg, true)
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

func respondInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, msg string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Flags: flags},
	})
}
