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
	title, description := extractPageMeta(html)
	if title != "OG Title" || description != "OG Description" {
		t.Fatalf("got title=%q description=%q", title, description)
	}

	fallback := `<html><head>
	<meta name="twitter:title" content="Twitter Title">
	<meta name="description" content="Meta Description">
	</head></html>`
	title, description = extractPageMeta(fallback)
	if title != "Twitter Title" || description != "Meta Description" {
		t.Fatalf("fallback got title=%q description=%q", title, description)
	}

	titleOnly := `<html><head><title>Just Title</title></head></html>`
	title, description = extractPageMeta(titleOnly)
	if title != "Just Title" || description != "" {
		t.Fatalf("title-only got title=%q description=%q", title, description)
	}
}

func TestExtractPageMetaTruncates(t *testing.T) {
	longTitle := strings.Repeat("あ", urlPageTitleMaxRunes+10)
	longDesc := strings.Repeat("い", urlPageDescriptionMaxRunes+10)
	html := `<html><head>
	<meta property="og:title" content="` + longTitle + `">
	<meta property="og:description" content="` + longDesc + `">
	</head></html>`
	title, description := extractPageMeta(html)
	if len([]rune(title)) != urlPageTitleMaxRunes {
		t.Fatalf("title runes = %d", len([]rune(title)))
	}
	if len([]rune(description)) != urlPageDescriptionMaxRunes {
		t.Fatalf("description runes = %d", len([]rune(description)))
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
	started := make(chan struct{}, urlPageLookupConcurrency+1)
	release := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-release
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	cache := newURLPageCache(client, 24*time.Hour, time.Now)
	done := make(chan string, 1)
	go func() {
		done <- cache.Replace(context.Background(), "https://one.example/a https://two.example/b https://three.example/c https://four.example/d https://five.example/e", "en")
	}()

	for i := 0; i < urlPageLookupConcurrency; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("URL lookups did not start in parallel")
		}
	}
	select {
	case <-started:
		t.Fatalf("more than %d URL lookups started concurrently", urlPageLookupConcurrency)
	default:
	}
	for i := 0; i < urlPageLookupConcurrency; i++ {
		release <- struct{}{}
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queued URL lookup did not start")
	}
	release <- struct{}{}
	select {
	case got := <-done:
		want := "https://one.example/a https://two.example/b https://three.example/c https://four.example/d https://five.example/e"
		if got != want {
			t.Fatalf("Replace() = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("Replace did not finish")
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
