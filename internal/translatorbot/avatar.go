package translatorbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net"
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

// avatarProxyURL routes an avatar through this service so the ring is drawn
// with the author's role color. Without a public base URL the CDN URL is
// returned untouched.
func avatarProxyURL(publicBaseURL, avatarURL string, roleColor int) string {
	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if publicBaseURL == "" || sanitizeWebhookAvatarURL(avatarURL) == "" {
		return avatarURL
	}
	v := url.Values{}
	v.Set("url", avatarURL)
	if roleColor > 0 {
		v.Set("color", avatarColorParam(roleColor))
	}
	return publicBaseURL + "/avatar?" + v.Encode()
}

// avatarColorParam encodes the ring-color query parameter written into the
// proxied URL by avatarProxyURL and decoded by avatarRingColor. This pair is
// the role color's single HTTP boundary.
func avatarColorParam(roleColor int) string {
	return fmt.Sprintf("%06X", roleColor&0xFFFFFF)
}

func NewAvatarHandler(client *http.Client, limiter *RateLimiter) http.Handler {
	if client == nil {
		panic("NewAvatarHandler requires a non-nil *http.Client")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Take(clientIP(r), 1) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		rawURL := r.URL.Query().Get("url")
		if sanitizeWebhookAvatarURL(rawURL) == "" {
			http.Error(w, "invalid avatar url", http.StatusBadRequest)
			return
		}
		img, err := fetchAvatarImage(r.Context(), client, rawURL)
		if err != nil {
			if errors.Is(err, errAvatarImageUnsupported) {
				http.Error(w, "unsupported avatar image", http.StatusUnsupportedMediaType)
				return
			}
			http.Error(w, "failed to fetch avatar", http.StatusBadGateway)
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

// errAvatarImageUnsupported marks a body the avatar proxy cannot render as an
// image, whether it exceeded its byte cap or failed to decode.
var errAvatarImageUnsupported = errors.New("unsupported avatar image")

// fetchAvatarImage is the avatar proxy's transport step: one outbound image
// fetch capped at avatarMaxBytes, then a decode. Rendering happens only after
// the transport step succeeds.
func fetchAvatarImage(ctx context.Context, client *http.Client, rawURL string) (image.Image, error) {
	content, err := fetchRemoteURL(ctx, client, rawURL, remoteFetchOptions{timeout: 6 * time.Second, maxBytes: avatarMaxBytes})
	if err != nil {
		if errors.Is(err, errRemoteContentTooLarge) {
			return nil, errAvatarImageUnsupported
		}
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(content.body))
	if err != nil {
		return nil, errAvatarImageUnsupported
	}
	return img, nil
}

func clientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return xff
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// avatarRingColor decodes the query parameter avatarColorParam encodes.
// Anything it cannot parse falls back to the default ring color.
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

	for y := range avatarSize {
		for x := range avatarSize {
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
