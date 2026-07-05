package main

import (
	"discord-auto-translator/internal/translatorbot"
	"github.com/bwmarrin/discordgo"
)

func memberRoleColor(s *discordgo.Session, guildID string, member *discordgo.Member) int {
	if s == nil || guildID == "" || member == nil || len(member.Roles) == 0 {
		return 0
	}
	guild, err := s.State.Guild(guildID)
	if err != nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			return 0
		}
	}
	if guild == nil || len(guild.Roles) == 0 {
		return 0
	}
	roles := make([]translatorbot.GuildRole, 0, len(guild.Roles))
	for _, role := range guild.Roles {
		if role == nil {
			continue
		}
		roles = append(roles, translatorbot.GuildRole{
			ID:       role.ID,
			Color:    role.Color,
			Position: role.Position,
		})
	}
	return translatorbot.HighestRoleColor(member.Roles, roles)
}
