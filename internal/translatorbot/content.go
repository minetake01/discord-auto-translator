package translatorbot

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const discordMessageContentLimit = 2000
const discordEpochMillis = 1420070400000

const (
	stickerFormatPNG    = 1
	stickerFormatAPNG   = 2
	stickerFormatLottie = 3
	stickerFormatGIF    = 4
)

func stickerAssetURL(sticker DiscordSticker) string {
	switch sticker.FormatType {
	case stickerFormatGIF:
		return fmt.Sprintf("https://media.discordapp.net/stickers/%s.gif", sticker.ID)
	default:
		return fmt.Sprintf("https://cdn.discordapp.com/stickers/%s.png", sticker.ID)
	}
}

// messageContentWithAssetURLs appends unsigned CDN URLs for attachments and
// stickers to the message body, enforcing Discord's content length limit.
func messageContentWithAssetURLs(content string, attachments []DiscordAttachment, stickers []DiscordSticker) (string, error) {
	assetURLs := make([]string, 0, len(attachments)+len(stickers))
	for _, attachment := range attachments {
		unsignedURL, err := unsignedAssetURL(attachment.URL)
		if err != nil {
			return "", fmt.Errorf("attachment %q: %w", attachmentFileName(attachment), err)
		}
		assetURLs = append(assetURLs, unsignedURL)
	}
	for _, sticker := range stickers {
		if strings.TrimSpace(sticker.ID) == "" {
			return "", errors.New("sticker ID is required")
		}
		assetURLs = append(assetURLs, stickerAssetURL(sticker))
	}
	if len(assetURLs) > 0 {
		if strings.TrimSpace(content) != "" {
			content += "\n"
		}
		content += strings.Join(assetURLs, "\n")
	}
	if utf8.RuneCountInString(content) > discordMessageContentLimit {
		return "", fmt.Errorf("message content has %d characters; Discord limit is %d", utf8.RuneCountInString(content), discordMessageContentLimit)
	}
	return content, nil
}

// unsignedAssetURL strips the signature query parameters from a Discord CDN
// URL so the link stays valid after the signature expires.
func unsignedAssetURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid HTTP URL %q", rawURL)
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String(), nil
}

func attachmentFileName(attachment DiscordAttachment) string {
	if name := filepath.Base(strings.TrimSpace(attachment.Filename)); name != "." && name != "/" && name != "\\" {
		return name
	}
	if u, err := url.Parse(strings.TrimSpace(attachment.URL)); err == nil {
		if name := filepath.Base(u.Path); name != "." && name != "/" && name != "\\" {
			return name
		}
	}
	return "attachment"
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

// normalizeMarkdownHeaderSnippet converts an ATX markdown header line into
// Discord subtext form for use in pseudo-reply snippets.
func normalizeMarkdownHeaderSnippet(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return line
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i >= len(line) || line[i] != ' ' {
		return line
	}
	title := strings.TrimSpace(line[i+1:])
	title = strings.TrimRight(title, " #")
	if title == "" {
		return line
	}
	return "-# " + title
}

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

func discordSnowflakeTime(id string) (time.Time, bool) {
	if len(id) < 17 {
		return time.Time{}, false
	}
	snowflake, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	timestampMillis := int64(snowflake>>22) + discordEpochMillis
	return time.UnixMilli(timestampMillis).UTC(), true
}

func snowflakeIDBefore(cutoff time.Time) string {
	return strconv.FormatUint((uint64(cutoff.UnixMilli()-discordEpochMillis)<<22), 10)
}

func truncateRunes(text string, maxRunes int, ellipsis string) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + ellipsis
}
