// SPEC 3.9: URL hreflang replacement and page metadata for translation context.
package translatorbot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func htmlResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestPickHreflangURL(t *testing.T) {
	html := `<html><head>
	<link rel="alternate" hreflang="ja" href="https://example.com/ja">
	<link rel="alternate" hreflang="en-US" href="https://example.com/en">
	</head></html>`
	hreflangs := extractHreflangURLs(html)
	if got := pickHreflangURL(hreflangs, "ja"); got != "https://example.com/ja" {
		t.Fatalf("got %q", got)
	}
	if got := pickHreflangURL(hreflangs, "en"); got != "https://example.com/en" {
		t.Fatalf("got %q", got)
	}
	if got := pickHreflangURL(hreflangs, "ko"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractPageMeta(t *testing.T) {
	html := `<html><head>
	<meta property="og:title" content="OG Title">
	<meta property="og:description" content="OG Description">
	<meta name="twitter:title" content="Twitter Title">
	<title>HTML Title</title>
	</head></html>`
	title, description, imageURL := extractPageMeta(html)
	if title != "OG Title" || description != "OG Description" {
		t.Fatalf("got title=%q description=%q", title, description)
	}
	if imageURL != "" {
		t.Fatalf("imageURL = %q, want empty", imageURL)
	}

	fallback := `<html><head>
	<meta name="twitter:title" content="Twitter Title">
	<meta name="description" content="Meta Description">
	</head></html>`
	title, description, imageURL = extractPageMeta(fallback)
	if title != "Twitter Title" || description != "Meta Description" {
		t.Fatalf("fallback got title=%q description=%q", title, description)
	}
	if imageURL != "" {
		t.Fatalf("fallback imageURL = %q, want empty", imageURL)
	}

	titleOnly := `<html><head><title>Just Title</title></head></html>`
	title, description, imageURL = extractPageMeta(titleOnly)
	if title != "Just Title" || description != "" || imageURL != "" {
		t.Fatalf("title-only got title=%q description=%q imageURL=%q", title, description, imageURL)
	}
}

func TestExtractPageMetaTruncates(t *testing.T) {
	longTitle := strings.Repeat("あ", urlPageTitleMaxRunes+10)
	longDesc := strings.Repeat("い", urlPageDescriptionMaxRunes+10)
	html := `<html><head>
	<meta property="og:title" content="` + longTitle + `">
	<meta property="og:description" content="` + longDesc + `">
	</head></html>`
	title, description, _ := extractPageMeta(html)
	titleRunes := len([]rune(title))
	descRunes := len([]rune(description))
	if titleRunes > urlPageTitleMaxRunes {
		t.Fatalf("title runes = %d, want at most %d", titleRunes, urlPageTitleMaxRunes)
	}
	if descRunes > urlPageDescriptionMaxRunes {
		t.Fatalf("description runes = %d, want at most %d", descRunes, urlPageDescriptionMaxRunes)
	}
	if titleRunes >= len([]rune(longTitle)) {
		t.Fatalf("title was not truncated: got %d runes from %d input runes", titleRunes, len([]rune(longTitle)))
	}
	if descRunes >= len([]rune(longDesc)) {
		t.Fatalf("description was not truncated: got %d runes from %d input runes", descRunes, len([]rune(longDesc)))
	}
}

func TestURLPageCacheCachesByURL(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<html><head>
		<meta property="og:title" content="Page">
		<link rel="alternate" hreflang="en" href="https://example.com/en` + req.URL.Path + `">
		</head></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)

	first := cache.Replace(context.Background(), "https://example.com/first", "en")
	second := cache.Replace(context.Background(), "https://example.com/first", "en")
	other := cache.Replace(context.Background(), "https://example.com/second", "en")

	if first != "https://example.com/en/first" || second != "https://example.com/en/first" {
		t.Fatalf("same URL replacements = %q, %q", first, second)
	}
	if other != "https://example.com/en/second" {
		t.Fatalf("other URL replacement = %q", other)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestURLPageCacheLookupReusedByReplace(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<html><head>
		<meta property="og:title" content="Article">
		<meta property="og:description" content="About the article">
		<link rel="alternate" hreflang="ja" href="https://example.com/ja">
		</head></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)

	pages := cache.Lookup(context.Background(), "see https://example.com/page")
	info := pages["https://example.com/page"]
	if info.Title != "Article" || info.Description != "About the article" {
		t.Fatalf("lookup info = %+v", info)
	}
	got := cache.Replace(context.Background(), "https://example.com/page", "ja")
	if got != "https://example.com/ja" {
		t.Fatalf("Replace() = %q", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestURLPageCacheSkipsDiscordHosts(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<html></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)
	pages := cache.Lookup(context.Background(), "https://discord.com/channels/1/2 https://cdn.discordapp.com/attachments/1/2/a.png")
	if len(pages) != 2 {
		t.Fatalf("pages = %d", len(pages))
	}
	for _, info := range pages {
		if info.Title != "" || len(info.Hreflangs) > 0 {
			t.Fatalf("unexpected info %+v", info)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestURLPageCacheRefreshesExpiredURL(t *testing.T) {
	now := time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, func() time.Time { return now })

	cache.Replace(context.Background(), "https://example.com/page", "en")
	now = now.Add(24 * time.Hour)
	cache.Replace(context.Background(), "https://example.com/page", "en")

	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestURLPageCacheDoesNotCacheRequestErrors(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return nil, errors.New("temporary failure")
		}
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)

	cache.Replace(context.Background(), "https://example.com/page", "en")
	cache.Replace(context.Background(), "https://example.com/page", "en")

	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestURLPageCacheConcurrentAccess(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Replace(context.Background(), "https://example.com/page", "en")
		}()
	}
	wg.Wait()
}

func TestURLPageCacheLooksUpDistinctURLsInParallelWithBound(t *testing.T) {
	var inFlight atomic.Int32
	var peak atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		current := inFlight.Add(1)
		for {
			old := peak.Load()
			if current <= old || peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		return htmlResponse(`<html><head>
		<meta property="og:title" content="` + req.URL.Host + `">
		<link rel="alternate" hreflang="en" href="https://example.com/en` + req.URL.Path + `">
		</head></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)

	content := "https://one.example/a https://two.example/b https://three.example/c"
	got := cache.Replace(context.Background(), content, "en")
	want := "https://example.com/en/a https://example.com/en/b https://example.com/en/c"
	if got != want {
		t.Fatalf("Replace() = %q, want %q", got, want)
	}
	if peak.Load() < 2 {
		t.Fatalf("expected more than one URL lookup in flight concurrently, peak = %d", peak.Load())
	}
}

func TestURLPageCacheLooksUpDuplicateURLOnce(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<link rel="alternate" hreflang="en" href="https://example.com/en">`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)

	got := cache.Replace(context.Background(), "https://example.com/page and https://example.com/page", "en")
	if got != "https://example.com/en and https://example.com/en" {
		t.Fatalf("Replace() = %q", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}
