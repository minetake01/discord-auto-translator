package translatorbot

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvatarWithLanguageBadgeBuildsEndpointURL(t *testing.T) {
	got := AvatarWithLanguageBadge(context.Background(), "https://bot.example.com/", "https://cdn.example.com/avatar.png?x=1", "ja", 0)
	if !strings.HasPrefix(got, "https://bot.example.com/avatar?url=") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "https%3A%2F%2Fcdn.example.com%2Favatar.png%3Fx%3D1") {
		t.Fatalf("avatar source URL was not escaped: %q", got)
	}
	if strings.Contains(got, "color=") {
		t.Fatalf("unexpected color query for zero role color: %q", got)
	}
}

func TestAvatarWithLanguageBadgeIncludesRoleColor(t *testing.T) {
	got := AvatarWithLanguageBadge(context.Background(), "https://bot.example.com/", "https://cdn.example.com/avatar.png", "ja", 0x5865F2)
	if !strings.Contains(got, "color=5865F2") {
		t.Fatalf("got %q, want color query", got)
	}
}

func TestAvatarWithLanguageBadgeFallsBackWithoutPublicBaseURL(t *testing.T) {
	const avatarURL = "https://cdn.example.com/avatar.png"
	got := AvatarWithLanguageBadge(context.Background(), "", avatarURL, "ja", 0x5865F2)
	if got != avatarURL {
		t.Fatalf("got %q, want %q", got, avatarURL)
	}
}

func TestAvatarHandlerReturnsPNGWithDefaultRing(t *testing.T) {
	source := newAvatarSourceServer(t)
	defer source.Close()

	req := httptest.NewRequest(http.MethodGet, "/avatar?url="+source.URL, nil)
	rec := httptest.NewRecorder()
	NewAvatarHandler(source.Client()).ServeHTTP(rec, req)

	assertAvatarPNGResponse(t, rec, avatarDefaultRingColor, color.RGBA{R: 20, G: 40, B: 60, A: 255})
}

func TestAvatarHandlerReturnsPNGWithCustomRingColor(t *testing.T) {
	source := newAvatarSourceServer(t)
	defer source.Close()

	req := httptest.NewRequest(http.MethodGet, "/avatar?url="+source.URL+"&color=5865F2", nil)
	rec := httptest.NewRecorder()
	NewAvatarHandler(source.Client()).ServeHTTP(rec, req)

	assertAvatarPNGResponse(t, rec, color.RGBA{R: 0x58, G: 0x65, B: 0xF2, A: 255}, color.RGBA{R: 20, G: 40, B: 60, A: 255})
}

func TestAvatarHandlerFallsBackToDefaultRingForInvalidColor(t *testing.T) {
	source := newAvatarSourceServer(t)
	defer source.Close()

	req := httptest.NewRequest(http.MethodGet, "/avatar?url="+source.URL+"&color=not-a-color", nil)
	rec := httptest.NewRecorder()
	NewAvatarHandler(source.Client()).ServeHTTP(rec, req)

	assertAvatarPNGResponse(t, rec, avatarDefaultRingColor, color.RGBA{R: 20, G: 40, B: 60, A: 255})
}

func newAvatarSourceServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				img.Set(x, y, color.RGBA{R: 20, G: 40, B: 60, A: 255})
			}
		}
		_ = png.Encode(w, img)
	}))
}

func assertAvatarPNGResponse(t *testing.T, rec *httptest.ResponseRecorder, ringColor, centerColor color.RGBA) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("got content type %q", got)
	}
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Dx(); got != avatarSize {
		t.Fatalf("got width %d", got)
	}
	if got := color.NRGBAModel.Convert(img.At(avatarSize/2, avatarBorderWidth/2)).(color.NRGBA); got.R != ringColor.R || got.G != ringColor.G || got.B != ringColor.B {
		t.Fatalf("ring pixel = %#v, want %#v", got, ringColor)
	}
	if got := color.NRGBAModel.Convert(img.At(avatarSize/2, avatarSize/2)).(color.NRGBA); got.R != centerColor.R || got.G != centerColor.G || got.B != centerColor.B {
		t.Fatalf("center pixel = %#v, want %#v", got, centerColor)
	}
}
