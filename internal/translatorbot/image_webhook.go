package translatorbot

import (
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
		if item.Vision.dataURL == "" {
			continue
		}
		vision = append(vision, item.Vision)
	}
	return vision
}

// visionBytesTotal sums the decoded vision payload sizes. Sizes are carried by
// visionImage from its construction, so this never decodes base64 again.
func visionBytesTotal(images []visionImage) int {
	total := 0
	for _, img := range images {
		total += img.payloadSize
	}
	return total
}
