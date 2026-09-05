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
	visionMaxEdge                     = 384
	visionJPEGQuality                 = 75
	imageFetchMaxBytes                = 8 * 1024 * 1024
	imageFetchTimeout                 = 8 * time.Second
	discordAttachmentDescriptionLimit = 1024
	visionTokenOverheadPerImage       = 160
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

type contextImageLocation struct {
	key        string
	attachment DiscordAttachment
	historyIdx int
	replyIdx   int
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

func sourceImageAttachments(m DiscordMessage) []DiscordAttachment {
	if m.ForwardedMessage != nil {
		return imageAttachmentsOnly(m.ForwardedMessage.Attachments)
	}
	return imageAttachmentsOnly(m.Attachments)
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

func (s *Service) loadImageAttachments(ctx context.Context, attachments []DiscordAttachment) []loadedImageAttachment {
	loaded := s.downloadImageOriginals(ctx, attachments)
	visionCount := 0
	totalVisionBytes := 0
	for i := range loaded {
		if visionCount >= visionMaxImages {
			break
		}
		jpegBytes, err := resizeImageForVision(loaded[i].Original)
		if err != nil || totalVisionBytes+len(jpegBytes) > visionMaxTotalBytes {
			continue
		}
		totalVisionBytes += len(jpegBytes)
		loaded[i].Vision = visionImage{DataURL: jpegDataURL(jpegBytes)}
		visionCount++
	}
	return loaded
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
