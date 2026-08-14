package translatorbot

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func largeTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 80, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestResizeImageForVisionCapsLongestEdge(t *testing.T) {
	original := largeTestJPEG(t, 1920, 1080)
	resized, err := resizeImageForVision(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatal(err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() > visionMaxEdge || bounds.Dy() > visionMaxEdge {
		t.Fatalf("resized to %dx%d, want longest edge <= %d", bounds.Dx(), bounds.Dy(), visionMaxEdge)
	}
	if bounds.Dx() != visionMaxEdge && bounds.Dy() != visionMaxEdge {
		t.Fatalf("resized to %dx%d, want one edge == %d", bounds.Dx(), bounds.Dy(), visionMaxEdge)
	}
}

func TestExtractPageMetaReadsOGImage(t *testing.T) {
	html := `<html><head>
	<meta property="og:title" content="OG Title">
	<meta property="og:image" content="/hero.jpg">
	<meta name="twitter:image" content="https://cdn.example/twitter.jpg">
	</head></html>`
	title, _, imageURL := extractPageMeta(html)
	if title != "OG Title" || imageURL != "/hero.jpg" {
		t.Fatalf("title=%q imageURL=%q", title, imageURL)
	}
}

func TestURLPageCacheResolvesRelativeOGImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>
		<meta property="og:title" content="Article">
		<meta property="og:image" content="/images/hero.png">
		</head></html>`))
	}))
	t.Cleanup(server.Close)
	cache := newURLPageCache(server.Client(), urlPageCacheTTL, nil)
	pages := cache.Lookup(context.Background(), "see "+server.URL+"/post")
	info := pages[server.URL+"/post"]
	if info.Title != "Article" {
		t.Fatalf("title = %q", info.Title)
	}
	if info.ImageURL != server.URL+"/images/hero.png" {
		t.Fatalf("imageURL = %q", info.ImageURL)
	}
}

func TestIsImageAttachmentUsesContentTypeAndExtension(t *testing.T) {
	if !isImageAttachment(DiscordAttachment{ContentType: "image/png"}) {
		t.Fatal("png content type should be an image")
	}
	if !isImageAttachment(DiscordAttachment{Filename: "shot.JPEG"}) {
		t.Fatal("jpeg extension should be an image")
	}
	if isImageAttachment(DiscordAttachment{Filename: "notes.zip", ContentType: "application/zip"}) {
		t.Fatal("zip should not be an image")
	}
}

func TestParseMultiTranslationResponseAttachmentDescriptions(t *testing.T) {
	p := NewProtector(NameMaps{})
	texts, alts, err := parseMultiTranslationResponse(
		`{"translations":[{"language":"en","translated_text":"Hello","attachment_descriptions":["Warning: doors closing",""]}]}`,
		[]string{"en"},
		p,
		"こんにちは",
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if texts["en"] != "Hello" {
		t.Fatalf("text = %q", texts["en"])
	}
	if len(alts["en"]) != 2 || alts["en"][0] != "Warning: doors closing" || alts["en"][1] != "" {
		t.Fatalf("alts = %#v", alts["en"])
	}

	if _, _, err := parseMultiTranslationResponse(
		`{"translations":[{"language":"en","translated_text":"Hello","attachment_descriptions":["only one"]}]}`,
		[]string{"en"},
		p,
		"こんにちは",
		2,
	); err == nil {
		t.Fatal("expected length mismatch error")
	}

	if _, _, err := parseMultiTranslationResponse(
		`{"translations":[{"language":"en","translated_text":"Hello"}]}`,
		[]string{"en"},
		p,
		"こんにちは",
		1,
	); err == nil {
		t.Fatal("expected missing attachment_descriptions error")
	}

	texts, alts, err = parseMultiTranslationResponse(
		`{"translations":[{"language":"en","translated_text":"Hello","attachment_descriptions":[]}]}`,
		[]string{"en"},
		p,
		"こんにちは",
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if texts["en"] != "Hello" || alts != nil {
		t.Fatalf("texts=%#v alts=%#v", texts, alts)
	}
}

func TestParseMultiTranslationResponseAllowsEmptyTextForImageOnly(t *testing.T) {
	p := NewProtector(NameMaps{})
	texts, alts, err := parseMultiTranslationResponse(
		`{"translations":[{"language":"en","translated_text":"","attachment_descriptions":["Exit"]}]}`,
		[]string{"en"},
		p,
		"",
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if texts["en"] != "" || alts["en"][0] != "Exit" {
		t.Fatalf("texts=%#v alts=%#v", texts, alts)
	}
}

func TestWebhookFilesForImagesTruncatesDescription(t *testing.T) {
	loaded := []loadedImageAttachment{{
		Attachment: DiscordAttachment{Filename: "a.png", ContentType: "image/png"},
		Original:   png1x1,
	}}
	files := webhookFilesForImages(loaded, []string{strings.Repeat("a", discordAttachmentDescriptionLimit+10)})
	if len(files) != 1 {
		t.Fatalf("files = %#v", files)
	}
	if got := []rune(files[0].Description); len(got) != discordAttachmentDescriptionLimit {
		t.Fatalf("description runes = %d", len(got))
	}
}

func TestTinyPNGDecodes(t *testing.T) {
	if _, err := png.Decode(bytes.NewReader(png1x1)); err != nil {
		t.Fatal(err)
	}
}
