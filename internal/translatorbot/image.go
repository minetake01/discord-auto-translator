package translatorbot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

func (s *Service) downloadImageOriginals(ctx context.Context, attachments []DiscordAttachment) []loadedImageAttachment {
	if len(attachments) == 0 {
		return nil
	}
	client := s.imageHTTPClient()
	loaded := make([]loadedImageAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		original, err := fetchImageBytes(ctx, client, attachment.URL)
		if err != nil {
			continue
		}
		loaded = append(loaded, loadedImageAttachment{Attachment: attachment, Original: original})
	}
	return loaded
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
			continue
		}
		item := loadedImageAttachment{Attachment: attachment, Original: original}
		jpegBytes, err := resizeImageForVision(original)
		if err == nil && totalVisionBytes+len(jpegBytes) <= visionMaxTotalBytes {
			totalVisionBytes += len(jpegBytes)
			item.Vision = visionImage{DataURL: jpegDataURL(jpegBytes)}
		}
		loaded = append(loaded, item)
	}
	return loaded, nil
}

func (s *Service) loadOGPVisionImages(ctx context.Context, sites []SiteContextEntry, remainingSlots int, remainingBytes int) []visionImage {
	if remainingSlots <= 0 || remainingBytes <= 0 {
		return nil
	}
	client := s.imageHTTPClient()
	out := make([]visionImage, 0, remainingSlots)
	for i, site := range sites {
		if remainingSlots <= 0 {
			break
		}
		jpegBytes, ok := s.tryLoadVisionJPEG(ctx, client, site.ImageURL, remainingBytes)
		if !ok {
			continue
		}
		remainingBytes -= len(jpegBytes)
		remainingSlots--
		sites[i].HasVisionImage = true
		out = append(out, visionImage{DataURL: jpegDataURL(jpegBytes)})
	}
	return out
}

type contextImageLocation struct {
	key        string
	attachment DiscordAttachment
	historyIdx int
	replyIdx   int
}

func (s *Service) loadContextVisionImages(ctx context.Context, tc *TranslationContext, remainingSlots, remainingBytes int) []visionImage {
	if tc == nil || remainingSlots <= 0 || remainingBytes <= 0 {
		return nil
	}
	locations := collectContextImageLocations(tc.History, tc.ReplyChain)
	if len(locations) == 0 {
		return nil
	}
	client := s.imageHTTPClient()
	loaded := make(map[int][]byte, remainingSlots)
	for _, locIdx := range contextImageLoadOrder(locations, len(tc.History), len(tc.ReplyChain)) {
		if remainingSlots <= 0 {
			break
		}
		jpegBytes, ok := s.tryLoadVisionJPEG(ctx, client, locations[locIdx].attachment.URL, remainingBytes)
		if !ok {
			continue
		}
		remainingBytes -= len(jpegBytes)
		remainingSlots--
		loaded[locIdx] = jpegBytes
	}
	indexed := make(map[int]int, len(loaded))
	vision := make([]visionImage, 0, len(loaded))
	nextIndex := 1
	assign := func(locIdx int) {
		jpegBytes, ok := loaded[locIdx]
		if !ok {
			return
		}
		if _, done := indexed[locIdx]; done {
			return
		}
		indexed[locIdx] = nextIndex
		vision = append(vision, visionImage{DataURL: jpegDataURL(jpegBytes)})
		nextIndex++
	}
	for i := range tc.History {
		for locIdx, loc := range locations {
			if loc.historyIdx == i {
				assign(locIdx)
			}
		}
	}
	for i := range tc.ReplyChain {
		for locIdx, loc := range locations {
			if loc.replyIdx == i {
				assign(locIdx)
			}
		}
	}
	stampContextImages(tc.History, locations, indexed, true)
	stampContextImages(tc.ReplyChain, locations, indexed, false)
	return vision
}

func collectContextImageLocations(history, reply []ChatContextMessage) []contextImageLocation {
	order := make([]contextImageLocation, 0)
	seen := make(map[string]int)
	add := func(msg ChatContextMessage, historyIdx, replyIdx int) {
		for _, attachment := range imageAttachmentsOnly(msg.Attachments) {
			url := strings.TrimSpace(attachment.URL)
			if url == "" {
				continue
			}
			key := msg.SourceChannelID + "\x00" + msg.SourceMessageID + "\x00" + url
			if i, ok := seen[key]; ok {
				if historyIdx >= 0 {
					order[i].historyIdx = historyIdx
				}
				if replyIdx >= 0 {
					order[i].replyIdx = replyIdx
				}
				continue
			}
			seen[key] = len(order)
			loc := contextImageLocation{key: key, attachment: attachment, historyIdx: -1, replyIdx: -1}
			if historyIdx >= 0 {
				loc.historyIdx = historyIdx
			}
			if replyIdx >= 0 {
				loc.replyIdx = replyIdx
			}
			order = append(order, loc)
		}
	}
	for i, msg := range history {
		add(msg, i, -1)
	}
	for i, msg := range reply {
		add(msg, -1, i)
	}
	return order
}

func contextImageLoadOrder(locations []contextImageLocation, historyLen, replyLen int) []int {
	order := make([]int, 0, len(locations))
	seen := make(map[int]bool, len(locations))
	appendMatching := func(match func(contextImageLocation) bool) {
		for i, loc := range locations {
			if seen[i] || !match(loc) {
				continue
			}
			seen[i] = true
			order = append(order, i)
		}
	}
	for i := replyLen - 1; i >= 0; i-- {
		idx := i
		appendMatching(func(loc contextImageLocation) bool { return loc.replyIdx == idx })
	}
	for i := historyLen - 1; i >= 0; i-- {
		idx := i
		appendMatching(func(loc contextImageLocation) bool { return loc.historyIdx == idx && loc.replyIdx < 0 })
	}
	return order
}

func stampContextImages(messages []ChatContextMessage, locations []contextImageLocation, indexed map[int]int, history bool) {
	for i := range messages {
		var images []TranslationAttachment
		for locIdx, loc := range locations {
			if history && loc.historyIdx != i {
				continue
			}
			if !history && loc.replyIdx != i {
				continue
			}
			index, ok := indexed[locIdx]
			if !ok {
				continue
			}
			images = append(images, TranslationAttachment{
				Index:       index,
				Filename:    attachmentFileName(loc.attachment),
				Description: strings.TrimSpace(loc.attachment.Description),
			})
		}
		messages[i].Images = images
	}
}

func (s *Service) tryLoadVisionJPEG(ctx context.Context, client *http.Client, rawURL string, remainingBytes int) ([]byte, bool) {
	imageURL := strings.TrimSpace(rawURL)
	if imageURL == "" || remainingBytes <= 0 {
		return nil, false
	}
	original, err := fetchImageBytes(ctx, client, imageURL)
	if err != nil {
		return nil, false
	}
	jpegBytes, err := resizeImageForVision(original)
	if err != nil || len(jpegBytes) == 0 || len(jpegBytes) > remainingBytes {
		return nil, false
	}
	return jpegBytes, true
}

func marshalImageAttachments(attachments []DiscordAttachment) (string, error) {
	if len(attachments) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal(attachments)
	if err != nil {
		return "", fmt.Errorf("marshal image attachments: %w", err)
	}
	return string(encoded), nil
}

func unmarshalImageAttachments(raw string) ([]DiscordAttachment, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var out []DiscordAttachment
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal image attachments: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
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

func sourceImageAttachments(m DiscordMessage) []DiscordAttachment {
	if m.ForwardedMessage != nil {
		return imageAttachmentsOnly(m.ForwardedMessage.Attachments)
	}
	return imageAttachmentsOnly(m.Attachments)
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
