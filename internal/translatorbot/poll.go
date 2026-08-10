package translatorbot

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const discordEmbedTitleLimit = 256

// formatTranslatedPollAnswers renders numbered answers using translated texts
// while keeping emoji markup from the source poll answers.
func formatTranslatedPollAnswers(poll *DiscordPoll, answers []string) string {
	if poll == nil {
		return ""
	}
	var b strings.Builder
	for i, text := range answers {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		if i < len(poll.Answers) {
			if emoji := pollEmojiMarkup(poll.Answers[i].Emoji); emoji != "" {
				b.WriteString(emoji)
				b.WriteByte(' ')
			}
		}
		b.WriteString(strings.TrimSpace(text))
	}
	return b.String()
}

// pollAnswerTexts returns trimmed answer texts in source order.
func pollAnswerTexts(poll *DiscordPoll) []string {
	if poll == nil {
		return nil
	}
	out := make([]string, len(poll.Answers))
	for i, answer := range poll.Answers {
		out[i] = strings.TrimSpace(answer.Text)
	}
	return out
}

// formatPollSnapshot is the plain-text snapshot used for reply quotes.
func formatPollSnapshot(poll *DiscordPoll) string {
	if poll == nil {
		return ""
	}
	question := strings.TrimSpace(poll.Question)
	answers := formatTranslatedPollAnswers(poll, pollAnswerTexts(poll))
	switch {
	case question != "" && answers != "":
		return question + "\n" + answers
	case question != "":
		return question
	default:
		return answers
	}
}

func pollEmojiMarkup(emoji *DiscordPollEmoji) string {
	if emoji == nil {
		return ""
	}
	name := strings.TrimSpace(emoji.Name)
	if emoji.ID != "" {
		if name == "" {
			name = "emoji"
		}
		if emoji.Animated {
			return fmt.Sprintf("<a:%s:%s>", name, emoji.ID)
		}
		return fmt.Sprintf("<:%s:%s>", name, emoji.ID)
	}
	return name
}

// buildPollEmbed builds the mirrored poll embed. Title is the question;
// description lists answers. Color is omitted when roleColor is 0.
func buildPollEmbed(question, answers string, roleColor int) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title:       truncateRunes(strings.TrimSpace(question), discordEmbedTitleLimit, ""),
		Description: strings.TrimSpace(answers),
	}
	if roleColor != 0 {
		embed.Color = roleColor
	}
	if embed.Title == "" && embed.Description == "" {
		return nil
	}
	return embed
}

// pollStartedHeader is the localized "poll started / vote here" line. It uses
// the same "> -# … · [label](url)" shape as pseudo-replies so quote/body
// helpers skip it. The vote URL always points at the source message.
func pollStartedHeader(language, guildID, channelID, messageID string) string {
	if messageID == "" {
		return ""
	}
	return fmt.Sprintf("> -# %s · [%s](%s)",
		localizedUIString(language, uiKeyPollStarted),
		localizedUIString(language, uiKeyPollVote),
		MessageJumpURL(guildID, channelID, messageID),
	)
}

// withPollStartedHeader prepends the poll started line to content.
func withPollStartedHeader(body, language, guildID, channelID, messageID string, hasPoll bool) string {
	if !hasPoll {
		return body
	}
	header := pollStartedHeader(language, guildID, channelID, messageID)
	if header == "" {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return header
	}
	return header + "\n" + body
}

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

// pollResultPercent returns the integer percent for victor votes over total,
// rounding half away from zero. totalVotes <= 0 yields 0.
func pollResultPercent(victorVotes, totalVotes int) int {
	if totalVotes <= 0 {
		return 0
	}
	return int(math.Round(float64(victorVotes) * 100 / float64(totalVotes)))
}

// formatPollVictorLabel builds the display label for a winning answer,
// combining optional emoji markup with the answer text.
func formatPollVictorLabel(answerText string, emoji *DiscordPollEmoji) string {
	answerText = strings.TrimSpace(answerText)
	emojiMarkup := pollEmojiMarkup(emoji)
	switch {
	case emojiMarkup != "" && answerText != "":
		return emojiMarkup + " " + answerText
	case answerText != "":
		return answerText
	default:
		return emojiMarkup
	}
}

// pollResultBody builds the mirrored poll-result body (no pseudo-reply).
// When the poll_result embed is missing, only the ended notice is returned.
func pollResultBody(language string, result *DiscordPollResult, victorLabel string) string {
	ended := localizedUIString(language, uiKeyPollEnded)
	if result == nil || !result.HasEmbed {
		return ended
	}
	if strings.TrimSpace(victorLabel) != "" {
		percent := strconv.Itoa(pollResultPercent(result.VictorAnswerVotes, result.TotalVotes))
		return ended + "\n" + localizedUIStringf(language, uiKeyPollResultVictor, victorLabel, percent)
	}
	return ended + "\n" + localizedUIString(language, uiKeyPollResultNoWinner)
}
