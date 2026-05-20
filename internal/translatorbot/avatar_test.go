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
	got := AvatarWithLanguageBadge(context.Background(), "https://bot.example.com/", "https://cdn.example.com/avatar.png?x=1", "ja")
	if !strings.HasPrefix(got, "https://bot.example.com/avatar?url=") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "https%3A%2F%2Fcdn.example.com%2Favatar.png%3Fx%3D1") {
		t.Fatalf("avatar source URL was not escaped: %q", got)
	}
}

func TestAvatarWithLanguageBadgeFallsBackWithoutPublicBaseURL(t *testing.T) {
	const avatarURL = "https://cdn.example.com/avatar.png"
	got := AvatarWithLanguageBadge(context.Background(), "", avatarURL, "ja")
	if got != avatarURL {
		t.Fatalf("got %q, want %q", got, avatarURL)
	}
}

func TestAvatarHandlerReturnsPNGWithOrangeRing(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				img.Set(x, y, color.RGBA{R: 20, G: 40, B: 60, A: 255})
			}
		}
		_ = png.Encode(w, img)
	}))
	defer source.Close()

	req := httptest.NewRequest(http.MethodGet, "/avatar?url="+source.URL, nil)
	rec := httptest.NewRecorder()
	NewAvatarHandler(source.Client()).ServeHTTP(rec, req)

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
	if got := color.NRGBAModel.Convert(img.At(avatarSize/2, avatarBorderWidth/2)).(color.NRGBA); got.R != avatarBorderColor.R || got.G != avatarBorderColor.G || got.B != avatarBorderColor.B {
		t.Fatalf("ring pixel = %#v", got)
	}
	if got := color.NRGBAModel.Convert(img.At(avatarSize/2, avatarSize/2)).(color.NRGBA); got.R != 20 || got.G != 40 || got.B != 60 {
		t.Fatalf("center pixel = %#v", got)
	}
}
