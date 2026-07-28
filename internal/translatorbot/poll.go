package translatorbot

import (
	"fmt"
	"strconv"
	"strings"
)

// formatPollBody renders a poll as plain text: question, then numbered answers.
// Answer emojis are preserved as Unicode or Discord custom-emoji markup.
func formatPollBody(poll *DiscordPoll) string {
	if poll == nil {
		return ""
	}
	var b strings.Builder
	if q := strings.TrimSpace(poll.Question); q != "" {
		b.WriteString(q)
	}
	for i, answer := range poll.Answers {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		if emoji := pollEmojiMarkup(answer.Emoji); emoji != "" {
			b.WriteString(emoji)
			b.WriteByte(' ')
		}
		b.WriteString(strings.TrimSpace(answer.Text))
	}
	return b.String()
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

// messageBodyForMirror is the source text mirrored and snapshotted: optional
// message content plus a textual rendering of any poll.
func messageBodyForMirror(m DiscordMessage) string {
	content := strings.TrimSpace(m.Content)
	pollBody := formatPollBody(m.Poll)
	switch {
	case content != "" && pollBody != "":
		return content + "\n\n" + pollBody
	case pollBody != "":
		return pollBody
	default:
		return m.Content
	}
}

// withPollStartedHeader prepends the localized "poll started / vote here" line
// after translation and link rewriting so the vote URL always points at the
// source message. The line uses the same "> -# … · [label](url)" shape as
// pseudo-replies so quote/body helpers skip it.
func withPollStartedHeader(body, language, guildID, channelID, messageID string, hasPoll bool) string {
	if !hasPoll || messageID == "" {
		return body
	}
	header := fmt.Sprintf("> -# %s · [%s](%s)",
		localizedUIString(language, uiKeyPollStarted),
		localizedUIString(language, uiKeyPollVote),
		MessageJumpURL(guildID, channelID, messageID),
	)
	if strings.TrimSpace(body) == "" {
		return header
	}
	return header + "\n\n" + body
}
