package translatorbot

import (
	"fmt"
	"regexp"
	"strings"
)

// timestampLinePattern matches the subtext line that backfilled mirrors start
// with to show when the source message was posted.
var timestampLinePattern = regexp.MustCompile(`^-# <t:\d+:f>$`)

func isTimestampLine(line string) bool {
	return timestampLinePattern.MatchString(strings.TrimSpace(line))
}

// withTimestampLine puts the source posting time on the first line; the
// existing headers and body start on the second line.
func withTimestampLine(sourceMessageID, content string) (string, error) {
	posted, ok := discordSnowflakeTime(sourceMessageID)
	if !ok {
		return "", fmt.Errorf("timestamp line: invalid message id %q", sourceMessageID)
	}
	line := fmt.Sprintf("-# <t:%d:f>", posted.Unix())
	if content == "" {
		return line, nil
	}
	return line + "\n" + content, nil
}

// mirrorText is a mirror body that already carries its timestamp line. Only
// content assembly produces it, and sends and edits accept nothing else, so a
// new path cannot skip the final step. Sends and edits split it into as many
// messages as the Discord content limit needs.
type mirrorText string

// finishMirrorContent turns an assembled mirror body into its final form: the
// timestamp line when the mirror shows the source posting time.
func finishMirrorContent(sourceMessageID, content string, timestamp bool) (mirrorText, error) {
	if timestamp {
		var err error
		if content, err = withTimestampLine(sourceMessageID, content); err != nil {
			return "", err
		}
	}
	return mirrorText(content), nil
}

func dropTimestampLine(lines []string) []string {
	if len(lines) > 0 && isTimestampLine(lines[0]) {
		return lines[1:]
	}
	return lines
}

// mirrorContentLines splits mirror text into lines with CRLF normalized, the
// form every bot-generated header is matched in.
func mirrorContentLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}

// mirroredMessageBody strips the bot-generated timestamp line, pseudo-reply
// quotes, and forwarded headers from the top of a mirrored message, leaving
// only the body.
func mirroredMessageBody(content string) string {
	lines := dropTimestampLine(mirrorContentLines(content))
	for len(lines) > 0 && (isPseudoReplyLine(lines[0]) || isForwardedHeaderLine(lines[0])) {
		lines = lines[1:]
		for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
			lines = lines[1:]
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func isForwardedHeaderLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "-# ") && strings.Contains(line, " · https://discord.com/channels/")
}

// isPseudoReplyLine recognizes only the exact bot-generated quote format
// ("> snippet · [label](message URL)") so user-authored blockquotes survive.
func isPseudoReplyLine(line string) bool {
	line = strings.TrimSpace(line)
	separator := strings.LastIndex(line, " · [")
	if !strings.HasPrefix(line, "> ") || separator < 2 || !strings.HasSuffix(line, ")") {
		return false
	}
	linkStart := strings.LastIndex(line[separator:], "](https://discord.com/channels/")
	return linkStart > 0
}

// firstLineWithoutPseudoReply returns the first non-empty line of a mirrored
// message, skipping a leading timestamp line and pseudo-reply quote line.
func firstLineWithoutPseudoReply(content string) string {
	lines := dropTimestampLine(mirrorContentLines(content))
	start := 0
	if len(lines) >= 1 && isPseudoReplyLine(lines[0]) {
		start = 1
	}
	for _, line := range lines[start:] {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// normalizeMarkdownHeaderSnippet converts a pseudo-reply snippet line into
// Discord subtext form. ATX markdown headers are normalized by stripping
// leading hashes; all other lines receive a -# prefix when absent.
func normalizeMarkdownHeaderSnippet(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") {
		i := 0
		for i < len(line) && line[i] == '#' {
			i++
		}
		if i < len(line) && line[i] == ' ' {
			title := strings.TrimSpace(line[i+1:])
			title = strings.TrimRight(title, " #")
			if title != "" {
				line = "-# " + title
			}
		}
	}
	if strings.HasPrefix(line, "-# ") {
		return line
	}
	return "-# " + line
}

// snippetURLPattern matches constructs that never produce embeds (masked
// links, already-suppressed URLs, inline code) so suppressSnippetEmbeds can
// leave them untouched, plus bare URLs that need wrapping.
var snippetURLPattern = regexp.MustCompile(`\[[^\[\]]*\]\([^()\s]*\)|<https?://[^\s<>()]+>|` + "`[^`]*`" + `|https?://[^\s<>()]+`)

// suppressSnippetEmbeds wraps bare URLs in <> so Discord does not render
// link embeds for a pseudo-reply snippet.
func suppressSnippetEmbeds(text string) string {
	return snippetURLPattern.ReplaceAllStringFunc(text, func(match string) string {
		if strings.HasPrefix(match, "http") {
			return "<" + match + ">"
		}
		return match
	})
}

const replyQuoteMaxRunes = 40

func withQuote(quote, content string) string {
	switch {
	case quote != "" && content != "":
		return quote + "\n\n" + content
	case quote != "":
		return quote
	default:
		return content
	}
}

func truncateRunes(text string, maxRunes int, ellipsis string) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + ellipsis
}
