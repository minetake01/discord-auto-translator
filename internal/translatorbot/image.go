package translatorbot

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	visionMaxImages                   = 4
	visionMaxTotalBytes               = 2 * 1024 * 1024
	visionMaxEdge                     = 768
	visionJPEGQuality                 = 75
	imageFetchMaxBytes                = 8 * 1024 * 1024
	imageFetchTimeout                 = 8 * time.Second
	discordAttachmentDescriptionLimit = 1024
	visionTokenOverheadPerImage       = 400
)

type visionImage struct {
	DataURL string
}

type loadedImageAttachment struct {
	Attachment DiscordAttachment
	Original   []byte
	Vision     visionImage
}

type TranslationAttachment struct {
	Index       int
	Filename    string
	Description string
}

func isImageAttachment(attachment DiscordAttachment) bool {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(attachment.ContentType, ";")[0]))
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	name := strings.ToLower(attachmentFileName(attachment))
	switch path.Ext(name) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

func imageAttachmentsOnly(attachments []DiscordAttachment) []DiscordAttachment {
	out := make([]DiscordAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if isImageAttachment(attachment) {
			out = append(out, attachment)
		}
	}
	return out
}

func nonImageAttachments(attachments []DiscordAttachment) []DiscordAttachment {
	out := make([]DiscordAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if !isImageAttachment(attachment) {
			out = append(out, attachment)
		}
	}
	return out
}

func (s *Service) downloadImageOriginals(ctx context.Context, attachments []DiscordAttachment) ([]loadedImageAttachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	client := s.imageHTTPClient()
	loaded := make([]loadedImageAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		original, err := fetchImageBytes(ctx, client, attachment.URL)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", attachmentFileName(attachment), err)
		}
		loaded = append(loaded, loadedImageAttachment{Attachment: attachment, Original: original})
	}
	return loaded, nil
}

func (s *Service) loadImageAttachments(ctx context.Context, attachments []DiscordAttachment) ([]loadedImageAttachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	if len(attachments) > visionMaxImages {
		return nil, fmt.Errorf("too many image attachments: %d (max %d)", len(attachments), visionMaxImages)
	}
	client := s.imageHTTPClient()
	loaded := make([]loadedImageAttachment, 0, len(attachments))
	totalVisionBytes := 0
	for _, attachment := range attachments {
		original, err := fetchImageBytes(ctx, client, attachment.URL)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", attachmentFileName(attachment), err)
		}
		jpegBytes, err := resizeImageForVision(original)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", attachmentFileName(attachment), err)
		}
		totalVisionBytes += len(jpegBytes)
		if totalVisionBytes > visionMaxTotalBytes {
			return nil, fmt.Errorf("resized image attachments exceed %d bytes", visionMaxTotalBytes)
		}
		loaded = append(loaded, loadedImageAttachment{
			Attachment: attachment,
			Original:   original,
			Vision:     visionImage{DataURL: jpegDataURL(jpegBytes)},
		})
	}
	return loaded, nil
}

func (s *Service) loadOGPVisionImages(ctx context.Context, sites []SiteContextEntry, remainingSlots int, remainingBytes int) []visionImage {
	if remainingSlots <= 0 || remainingBytes <= 0 {
		return nil
	}
	client := s.imageHTTPClient()
	out := make([]visionImage, 0, remainingSlots)
	for _, site := range sites {
		if remainingSlots <= 0 {
			break
		}
		imageURL := strings.TrimSpace(site.ImageURL)
		if imageURL == "" {
			continue
		}
		original, err := fetchImageBytes(ctx, client, imageURL)
		if err != nil {
			continue
		}
		jpegBytes, err := resizeImageForVision(original)
		if err != nil || len(jpegBytes) == 0 || len(jpegBytes) > remainingBytes {
			continue
		}
		remainingBytes -= len(jpegBytes)
		remainingSlots--
		out = append(out, visionImage{DataURL: jpegDataURL(jpegBytes)})
	}
	return out
}

func (s *Service) imageHTTPClient() *http.Client {
	if s != nil && s.httpClient != nil {
		return s.httpClient
	}
	return http.DefaultClient
}

func fetchImageBytes(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid HTTP URL %q", rawURL)
	}
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, imageFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create image request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch image: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, imageFetchMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(body) > imageFetchMaxBytes {
		return nil, fmt.Errorf("image exceeds %d bytes", imageFetchMaxBytes)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("image is empty")
	}
	return body, nil
}

func resizeImageForVision(original []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(original))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("image has invalid dimensions %dx%d", width, height)
	}
	resized := src
	if width > visionMaxEdge || height > visionMaxEdge {
		scale := float64(visionMaxEdge) / float64(width)
		if height > width {
			scale = float64(visionMaxEdge) / float64(height)
		}
		nextWidth := max(1, int(float64(width)*scale+0.5))
		nextHeight := max(1, int(float64(height)*scale+0.5))
		dst := image.NewRGBA(image.Rect(0, 0, nextWidth, nextHeight))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)
		resized = dst
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, resized, &jpeg.Options{Quality: visionJPEGQuality}); err != nil {
		return nil, fmt.Errorf("encode vision jpeg: %w", err)
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("encoded vision jpeg is empty")
	}
	return out.Bytes(), nil
}

func jpegDataURL(jpegBytes []byte) string {
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegBytes)
}

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

func sourceImageDescriptions(attachments []DiscordAttachment) []string {
	images := imageAttachmentsOnly(attachments)
	if len(images) == 0 {
		return nil
	}
	out := make([]string, len(images))
	for i, attachment := range images {
		out[i] = strings.TrimSpace(attachment.Description)
	}
	return out
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
		description := ""
		if i < len(descriptions) {
			description = truncateRunes(strings.TrimSpace(descriptions[i]), discordAttachmentDescriptionLimit, "")
		}
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
