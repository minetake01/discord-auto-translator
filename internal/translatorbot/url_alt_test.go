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

func TestExtractAlternateURL(t *testing.T) {
	html := `<html><head>
	<link rel="alternate" hreflang="ja" href="https://example.com/ja">
	<link rel="alternate" hreflang="en-US" href="https://example.com/en">
	</head></html>`
	if got := ExtractAlternateURL(html, "ja"); got != "https://example.com/ja" {
		t.Fatalf("got %q", got)
	}
	if got := ExtractAlternateURL(html, "en"); got != "https://example.com/en" {
		t.Fatalf("got %q", got)
	}
	if got := ExtractAlternateURL(html, "ko"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestAlternateURLReplacerCachesDomainWithoutAlternates(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, time.Now)

	replacer.Replace(context.Background(), "https://example.com:8080/first", "en")
	replacer.Replace(context.Background(), "https://EXAMPLE.com:9090/second", "en")

	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestAlternateURLReplacerCachesDomainsIndependently(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, time.Now)

	replacer.Replace(context.Background(), "https://one.example/page", "en")
	replacer.Replace(context.Background(), "https://two.example/page", "en")

	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestAlternateURLReplacerStillFetchesKnownDomainWithAlternates(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<link rel="alternate" hreflang="en" href="https://example.com/en` + req.URL.Path + `">`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, time.Now)

	first := replacer.Replace(context.Background(), "https://example.com/first", "en")
	second := replacer.Replace(context.Background(), "https://example.com/second", "en")

	if first != "https://example.com/en/first" || second != "https://example.com/en/second" {
		t.Fatalf("replacements = %q, %q", first, second)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestAlternateURLReplacerRefreshesExpiredDomain(t *testing.T) {
	now := time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, func() time.Time { return now })

	replacer.Replace(context.Background(), "https://example.com/first", "en")
	now = now.Add(24 * time.Hour)
	replacer.Replace(context.Background(), "https://example.com/second", "en")

	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestAlternateURLReplacerDoesNotCacheRequestErrors(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return nil, errors.New("temporary failure")
		}
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, time.Now)

	replacer.Replace(context.Background(), "https://example.com/first", "en")
	replacer.Replace(context.Background(), "https://example.com/second", "en")

	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

func TestAlternateURLReplacerConcurrentAccess(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replacer.Replace(context.Background(), "https://example.com/page", "en")
		}()
	}
	wg.Wait()
}

func TestAlternateURLReplacerLooksUpDistinctURLsInParallelWithBound(t *testing.T) {
	started := make(chan struct{}, alternateURLLookupConcurrency+1)
	release := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-release
		return htmlResponse(`<html><head></head></html>`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, time.Now)
	done := make(chan string, 1)
	go func() {
		done <- replacer.Replace(context.Background(), "https://one.example/a https://two.example/b https://three.example/c https://four.example/d https://five.example/e", "en")
	}()

	for i := 0; i < alternateURLLookupConcurrency; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("URL lookups did not start in parallel")
		}
	}
	select {
	case <-started:
		t.Fatalf("more than %d URL lookups started concurrently", alternateURLLookupConcurrency)
	default:
	}
	for i := 0; i < alternateURLLookupConcurrency; i++ {
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

func TestAlternateURLReplacerLooksUpDuplicateURLOnce(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return htmlResponse(`<link rel="alternate" hreflang="en" href="https://example.com/en">`), nil
	})}
	replacer := newAlternateURLReplacer(client, 24*time.Hour, time.Now)

	got := replacer.Replace(context.Background(), "https://example.com/page and https://example.com/page", "en")
	if got != "https://example.com/en and https://example.com/en" {
		t.Fatalf("Replace() = %q", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}
