package translatorbot

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	avatarSize        = 128
	avatarBorderWidth = 8
	avatarMaxBytes    = 2 * 1024 * 1024
)

var avatarDefaultRingColor = color.RGBA{R: 114, G: 118, B: 125, A: 255}

var avatarHexColorPattern = regexp.MustCompile(`(?i)^[0-9a-f]{6}$`)

func AvatarWithLanguageBadge(ctx context.Context, publicBaseURL, avatarURL, language string, roleColor int) string {
	_ = ctx
	_ = language
	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if publicBaseURL == "" || sanitizeWebhookAvatarURL(avatarURL) == "" {
		return avatarURL
	}
	v := url.Values{}
	v.Set("url", avatarURL)
	if roleColor > 0 {
		v.Set("color", fmt.Sprintf("%06X", roleColor&0xFFFFFF))
	}
	return publicBaseURL + "/avatar?" + v.Encode()
}

func NewAvatarHandler(client *http.Client) http.Handler {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawURL := r.URL.Query().Get("url")
		if sanitizeWebhookAvatarURL(rawURL) == "" {
			http.Error(w, "invalid avatar url", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			http.Error(w, "invalid avatar url", http.StatusBadRequest)
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "failed to fetch avatar", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			http.Error(w, "failed to fetch avatar", http.StatusBadGateway)
			return
		}
		img, _, err := image.Decode(http.MaxBytesReader(w, resp.Body, avatarMaxBytes))
		if err != nil {
			http.Error(w, "unsupported avatar image", http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if err := png.Encode(w, renderAvatarWithRing(img, avatarRingColor(r.URL.Query().Get("color")))); err != nil {
			http.Error(w, "failed to encode avatar", http.StatusInternalServerError)
			return
		}
	})
}

func avatarRingColor(raw string) color.Color {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return avatarDefaultRingColor
	}
	if !avatarHexColorPattern.MatchString(raw) {
		return avatarDefaultRingColor
	}
	value, err := strconv.ParseUint(raw, 16, 24)
	if err != nil {
		return avatarDefaultRingColor
	}
	return color.RGBA{
		R: uint8(value >> 16),
		G: uint8(value >> 8),
		B: uint8(value),
		A: 255,
	}
}

func renderAvatarWithRing(src image.Image, borderColor color.Color) image.Image {
	out := image.NewNRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	bounds := src.Bounds()
	cx := float64(avatarSize-1) / 2
	cy := float64(avatarSize-1) / 2
	outer := float64(avatarSize) / 2
	inner := outer - avatarBorderWidth

	for y := 0; y < avatarSize; y++ {
		for x := 0; x < avatarSize; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			dist2 := dx*dx + dy*dy
			if dist2 > outer*outer {
				continue
			}
			if dist2 >= inner*inner {
				out.Set(x, y, borderColor)
				continue
			}
			sx := bounds.Min.X + x*bounds.Dx()/avatarSize
			sy := bounds.Min.Y + y*bounds.Dy()/avatarSize
			out.Set(x, y, src.At(sx, sy))
		}
	}
	return out
}
