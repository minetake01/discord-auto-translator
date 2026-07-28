package translatorbot

import (
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

func sourceTypeChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Bot", Value: string(SourceTypeBot)},
		{Name: "Webhook", Value: string(SourceTypeWebhook)},
	}
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
