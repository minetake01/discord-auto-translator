package translatorbot

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const discordMessageContentLimit = 2000

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

// messageContentWithAssetURLs appends unsigned CDN URLs for non-image
// attachments and stickers to the message body. Image attachments are
// reuploaded separately with alt text.
func messageContentWithAssetURLs(content string, attachments []DiscordAttachment, stickers []DiscordSticker) (string, error) {
	return messageContentWithAssetURLsOption(content, attachments, stickers, false)
}

func messageContentWithAllAssetURLs(content string, attachments []DiscordAttachment, stickers []DiscordSticker) (string, error) {
	return messageContentWithAssetURLsOption(content, attachments, stickers, true)
}

func messageContentWithAssetURLsOption(content string, attachments []DiscordAttachment, stickers []DiscordSticker, includeImages bool) (string, error) {
	assetURLs := make([]string, 0, len(attachments)+len(stickers))
	for _, attachment := range attachments {
		if !includeImages && isImageAttachment(attachment) {
			continue
		}
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
