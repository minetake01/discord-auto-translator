package translatorbot

type GuildRole struct {
	ID       string
	Color    int
	Position int
}

func HighestRoleColor(memberRoleIDs []string, guildRoles []GuildRole) int {
	if len(memberRoleIDs) == 0 || len(guildRoles) == 0 {
		return 0
	}
	rolesByID := make(map[string]GuildRole, len(guildRoles))
	for _, role := range guildRoles {
		rolesByID[role.ID] = role
	}
	var best *GuildRole
	for _, roleID := range memberRoleIDs {
		role, ok := rolesByID[roleID]
		if !ok || role.Color == 0 {
			continue
		}
		if best == nil || role.Position > best.Position {
			roleCopy := role
			best = &roleCopy
		}
	}
	if best == nil {
		return 0
	}
	return best.Color
}
