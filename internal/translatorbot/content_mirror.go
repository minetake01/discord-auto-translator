package translatorbot

import (
	"strings"
)

// mirroredMessageBody strips bot-generated pseudo-reply quotes and forwarded
// headers from the top of a mirrored message, leaving only the body.
func mirroredMessageBody(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
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
// message, skipping a leading bot-generated pseudo-reply quote line.
func firstLineWithoutPseudoReply(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
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
