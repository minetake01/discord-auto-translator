package translatorbot

import (
	"encoding/base64"
	"net/http"
	"strings"
)

func translationAttachmentsFromLoaded(loaded []loadedImageAttachment) []TranslationAttachment {
	if len(loaded) == 0 {
		return nil
	}
	out := make([]TranslationAttachment, len(loaded))
	for i, item := range loaded {
		out[i] = TranslationAttachment{
			Index:       i + 1,
			Filename:    attachmentFileName(item.Attachment),
			Description: strings.TrimSpace(item.Attachment.Description),
		}
	}
	return out
}

func translatableAttachmentCount(attachments []TranslationAttachment) int {
	n := 0
	for _, attachment := range attachments {
		if hasTranslatableText(attachment.Description) {
			n++
		}
	}
	return n
}

func imagesNotReuploaded(attachments []DiscordAttachment, loaded []loadedImageAttachment) []DiscordAttachment {
	ok := make(map[string]struct{}, len(loaded))
	for _, item := range loaded {
		if url := strings.TrimSpace(item.Attachment.URL); url != "" {
			ok[url] = struct{}{}
		}
	}
	skipped := make([]DiscordAttachment, 0)
	for _, attachment := range imageAttachmentsOnly(attachments) {
		if _, have := ok[strings.TrimSpace(attachment.URL)]; have {
			continue
		}
		skipped = append(skipped, attachment)
	}
	return skipped
}

func messageContentWithLoadedImages(content string, attachments []DiscordAttachment, stickers []DiscordSticker, loaded []loadedImageAttachment) (string, error) {
	content, err := messageContentWithAssetURLs(content, attachments, stickers)
	if err != nil {
		return "", err
	}
	return messageContentWithAllAssetURLs(content, imagesNotReuploaded(attachments, loaded), nil)
}

func attachmentsFromLoaded(loaded []loadedImageAttachment) []DiscordAttachment {
	if len(loaded) == 0 {
		return nil
	}
	out := make([]DiscordAttachment, len(loaded))
	for i, item := range loaded {
		out[i] = item.Attachment
	}
	return out
}

func webhookFilesForImages(loaded []loadedImageAttachment, descriptions []string) []WebhookFile {
	if len(loaded) == 0 {
		return nil
	}
	out := make([]WebhookFile, len(loaded))
	for i, item := range loaded {
		description := strings.TrimSpace(item.Attachment.Description)
		if descriptions != nil {
			description = ""
			if i < len(descriptions) {
				description = strings.TrimSpace(descriptions[i])
			}
		}
		description = truncateRunes(description, discordAttachmentDescriptionLimit, "")
		contentType := strings.TrimSpace(item.Attachment.ContentType)
		if contentType == "" {
			contentType = http.DetectContentType(item.Original)
		}
		out[i] = WebhookFile{
			Name:        attachmentFileName(item.Attachment),
			ContentType: contentType,
			Description: description,
			Data:        item.Original,
		}
	}
	return out
}

func visionFromLoaded(loaded []loadedImageAttachment) []visionImage {
	if len(loaded) == 0 {
		return nil
	}
	vision := make([]visionImage, 0, len(loaded))
	for _, item := range loaded {
		if item.Vision.DataURL == "" {
			continue
		}
		vision = append(vision, item.Vision)
	}
	return vision
}

func visionBytesTotal(images []visionImage) int {
	total := 0
	for _, img := range images {
		if i := strings.Index(img.DataURL, ","); i >= 0 {
			decoded, err := base64.StdEncoding.DecodeString(img.DataURL[i+1:])
			if err == nil {
				total += len(decoded)
				continue
			}
		}
		total += len(img.DataURL)
	}
	return total
}
