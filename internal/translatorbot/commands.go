package translatorbot

import (
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// defaultAdminCommandPermissions is the Discord-side default for setup
// commands. Handlers do not re-check permissions; extra grants are configured
// in the server's application command permissions UI. View Original is exempt.
var defaultAdminCommandPermissions int64 = discordgo.PermissionAdministrator

const (
	viewOriginalCommandName = "View Original"
	botWhitelistCommandName = "bot-whitelist"
)

// Option kinds used by the command specs below.
const (
	optionKindString     = discordgo.ApplicationCommandOptionString
	optionKindBoolean    = discordgo.ApplicationCommandOptionBoolean
	optionKindChannel    = discordgo.ApplicationCommandOptionChannel
	optionKindSubCommand = discordgo.ApplicationCommandOptionSubCommand
)

// guildChannelTypes selects the channels and forums that can join a group.
var guildChannelTypes = []discordgo.ChannelType{
	discordgo.ChannelTypeGuildText,
	discordgo.ChannelTypeGuildNews,
	discordgo.ChannelTypeGuildForum,
	discordgo.ChannelTypeGuildMedia,
}

// glossaryAttributeKeys lists the attribute suggestions in display order.
var glossaryAttributeKeys = []uiKey{
	uiKeyGlossaryAttributePersonName,
	uiKeyGlossaryAttributePlaceName,
	uiKeyGlossaryAttributeSlang,
	uiKeyGlossaryAttributeAbbreviation,
	uiKeyGlossaryAttributeTechnicalTerm,
}

// commandOptionSpec declares one option. Descriptions are catalog keys, so the
// text Discord stores for the default locale and every localized variant come
// from the same entry.
type commandOptionSpec struct {
	name         string
	description  uiKey
	kind         discordgo.ApplicationCommandOptionType
	required     bool
	autocomplete bool
	channelTypes []discordgo.ChannelType
	choices      []*discordgo.ApplicationCommandOptionChoice
	options      []commandOptionSpec
}

func (o commandOptionSpec) discordgo() *discordgo.ApplicationCommandOption {
	option := &discordgo.ApplicationCommandOption{
		Name:                     o.name,
		Description:              commandText(o.description),
		DescriptionLocalizations: commandLocalizations(o.description),
		Type:                     o.kind,
		ChannelTypes:             o.channelTypes,
		Required:                 o.required,
		Autocomplete:             o.autocomplete,
		Choices:                  o.choices,
	}
	for _, child := range o.options {
		option.Options = append(option.Options, child.discordgo())
	}
	return option
}

// commandSpec declares one slash command.
type commandSpec struct {
	name        string
	description uiKey
	options     []commandOptionSpec
}

func (c commandSpec) discordgo() *discordgo.ApplicationCommand {
	command := &discordgo.ApplicationCommand{
		Name:                     c.name,
		Description:              commandText(c.description),
		DescriptionLocalizations: commandDescriptionLocalizations(c.description),
	}
	for _, option := range c.options {
		command.Options = append(command.Options, option.discordgo())
	}
	return command
}

// guildCommandSpecs lists the commands registered in every guild. Option names
// stay in ASCII because Discord keeps them untranslated.
var guildCommandSpecs = []commandSpec{
	{
		name:        "new-channel",
		description: uiKeyCmdNewChannelDesc,
		options: []commandOptionSpec{
			{name: "language", description: uiKeyOptLanguageDesc, kind: optionKindString, required: true, autocomplete: true},
			{name: "channel", description: uiKeyOptChannelRegisterDesc, kind: optionKindChannel, channelTypes: guildChannelTypes},
			{name: "group", description: uiKeyOptGroupIDDesc, kind: optionKindString},
		},
	},
	{
		name:        "join-channel",
		description: uiKeyCmdJoinChannelDesc,
		options: []commandOptionSpec{
			{name: "group", description: uiKeyOptExistingGroupDesc, kind: optionKindString, required: true, autocomplete: true},
			{name: "language", description: uiKeyOptLanguageDesc, kind: optionKindString, required: true, autocomplete: true},
			{name: "channel", description: uiKeyOptChannelJoinDesc, kind: optionKindChannel, channelTypes: guildChannelTypes},
			{name: "backfill", description: uiKeyOptBackfillToggleDesc, kind: optionKindBoolean},
		},
	},
	{
		name:        "backfill",
		description: uiKeyCmdBackfillDesc,
		options: []commandOptionSpec{
			{
				name:        "stop",
				description: uiKeyCmdBackfillStopDesc,
				kind:        optionKindSubCommand,
				options: []commandOptionSpec{
					{name: "channel", description: uiKeyOptChannelBackfillStopDesc, kind: optionKindChannel, channelTypes: guildChannelTypes},
				},
			},
		},
	},
	{
		name:        "leave-channel",
		description: uiKeyCmdLeaveChannelDesc,
		options: []commandOptionSpec{
			{name: "group", description: uiKeyOptExistingGroupDesc, kind: optionKindString, required: true, autocomplete: true},
			{name: "channel", description: uiKeyOptChannelRemoveDesc, kind: optionKindChannel, channelTypes: guildChannelTypes},
		},
	},
	{
		name:        "delete-group",
		description: uiKeyCmdDeleteGroupDesc,
		options: []commandOptionSpec{
			{name: "group", description: uiKeyOptExistingGroupDesc, kind: optionKindString, required: true, autocomplete: true},
		},
	},
	{
		name:        "add-glossary",
		description: uiKeyCmdAddGlossaryDesc,
		options: []commandOptionSpec{
			{name: "term", description: uiKeyOptTermMatchDesc, kind: optionKindString, required: true},
			{name: "translation", description: uiKeyOptTranslationDesc, kind: optionKindString, required: true},
			{name: "attribute", description: uiKeyOptAttributeDesc, kind: optionKindString, autocomplete: true},
			{name: "always_include", description: uiKeyOptAlwaysIncludeDesc, kind: optionKindBoolean},
		},
	},
	{name: "list-groups", description: uiKeyCmdListGroupsDesc},
	{name: "list-glossary", description: uiKeyCmdListGlossaryDesc},
	{
		name:        "remove-glossary",
		description: uiKeyCmdRemoveGlossaryDesc,
		options: []commandOptionSpec{
			{name: "term", description: uiKeyOptTermRemoveDesc, kind: optionKindString, required: true},
		},
	},
	{
		name:        "set-style",
		description: uiKeyCmdSetStyleDesc,
		options: []commandOptionSpec{
			{name: "group", description: uiKeyOptExistingGroupDesc, kind: optionKindString, required: true, autocomplete: true},
			{name: "preset", description: uiKeyOptStylePresetDesc, kind: optionKindString, choices: StylePresetChoices()},
			{name: "custom", description: uiKeyOptStyleCustomDesc, kind: optionKindString},
		},
	},
	{
		name:        editForumTagsCommand,
		description: uiKeyCmdEditForumTagsDesc,
		options: []commandOptionSpec{
			{name: "group", description: uiKeyOptExistingGroupDesc, kind: optionKindString, required: true, autocomplete: true},
			{name: "channel", description: uiKeyOptForumChannelDesc, kind: optionKindChannel, channelTypes: []discordgo.ChannelType{
				discordgo.ChannelTypeGuildForum,
				discordgo.ChannelTypeGuildMedia,
			}},
		},
	},
	{
		name:        botWhitelistCommandName,
		description: uiKeyCmdBotWhitelistDesc,
		options: []commandOptionSpec{
			{
				name:        "add",
				description: uiKeyCmdWhitelistAddDesc,
				kind:        optionKindSubCommand,
				options: []commandOptionSpec{
					{name: "source_type", description: uiKeyOptSourceTypeDesc, kind: optionKindString, required: true, choices: sourceTypeChoices()},
					{name: "source_id", description: uiKeyOptSourceIDDesc, kind: optionKindString, required: true},
				},
			},
			{
				name:        "remove",
				description: uiKeyCmdWhitelistRmDesc,
				kind:        optionKindSubCommand,
				options: []commandOptionSpec{
					{name: "source_type", description: uiKeyOptSourceTypeDesc, kind: optionKindString, required: true, choices: sourceTypeChoices()},
					{name: "source_id", description: uiKeyOptSourceIDDesc, kind: optionKindString, required: true},
				},
			},
			{name: "list", description: uiKeyCmdWhitelistListDesc, kind: optionKindSubCommand},
		},
	},
}

func sourceTypeChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{
			Name:              commandText(uiKeyChoiceSourceTypeBot),
			NameLocalizations: commandLocalizations(uiKeyChoiceSourceTypeBot),
			Value:             string(SourceTypeBot),
		},
		{
			Name:              commandText(uiKeyChoiceSourceTypeWebhook),
			NameLocalizations: commandLocalizations(uiKeyChoiceSourceTypeWebhook),
			Value:             string(SourceTypeWebhook),
		},
	}
}

func glossaryAttributeSuggestions(language, query string, limit int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]string, 0, len(glossaryAttributeKeys))
	for _, key := range glossaryAttributeKeys {
		attribute := localizedUIString(language, key)
		if query == "" || strings.Contains(strings.ToLower(attribute), query) {
			result = append(result, attribute)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func Commands() []*discordgo.ApplicationCommand {
	commands := make([]*discordgo.ApplicationCommand, 0, len(guildCommandSpecs)+1)
	for _, spec := range guildCommandSpecs {
		command := spec.discordgo()
		command.DefaultMemberPermissions = &defaultAdminCommandPermissions
		commands = append(commands, command)
	}
	return append(commands, &discordgo.ApplicationCommand{
		Name: viewOriginalCommandName,
		Type: discordgo.MessageApplicationCommand,
	})
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
