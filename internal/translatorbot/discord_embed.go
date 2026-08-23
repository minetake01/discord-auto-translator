package translatorbot

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// contentWithEmbedQuoteText merges message content with the first embed's
// title (or description first line) so reply quotes can see poll questions
// that live only in embeds.
func contentWithEmbedQuoteText(content string, embeds []*discordgo.MessageEmbed) string {
	embedLine := ""
	for _, embed := range embeds {
		if embed == nil {
			continue
		}
		if title := strings.TrimSpace(embed.Title); title != "" {
			embedLine = title
			break
		}
		if desc := strings.TrimSpace(embed.Description); desc != "" {
			if line, _, _ := strings.Cut(desc, "\n"); strings.TrimSpace(line) != "" {
				embedLine = strings.TrimSpace(line)
				break
			}
		}
	}
	content = strings.TrimRight(content, "\r\n")
	if embedLine == "" {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return embedLine
	}
	return content + "\n" + embedLine
}
