package translatorbot

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const alternateURLDomainCacheTTL = 24 * time.Hour

var urlPattern = regexp.MustCompile(`https?://[^\s<>()]+`)
var alternateLinkPattern = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*\balternate\b[^"']*["'][^>]*>`)
var attrPattern = regexp.MustCompile(`(?is)(href|hreflang|content|property)=["']([^"']+)["']`)
var ogLocalePattern = regexp.MustCompile(`(?is)<meta\s+[^>]*(property=["']og:locale:alternate["'][^>]*content=["']([^"']+)["']|content=["']([^"']+)["'][^>]*property=["']og:locale:alternate["'])[^>]*>`)

type alternateURLDomainCacheEntry struct {
	hasAlternates bool
	expiresAt     time.Time
}

type alternateURLReplacer struct {
	client *http.Client
	ttl    time.Duration
	now    func() time.Time

	mu      sync.Mutex
	domains map[string]alternateURLDomainCacheEntry
}

func newAlternateURLReplacer(client *http.Client, ttl time.Duration, now func() time.Time) *alternateURLReplacer {
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &alternateURLReplacer{
		client:  client,
		ttl:     ttl,
		now:     now,
		domains: make(map[string]alternateURLDomainCacheEntry),
	}
}

func (r *alternateURLReplacer) Replace(ctx context.Context, text, targetLanguage string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(rawURL string) string {
		domain, err := alternateURLDomain(rawURL)
		if err != nil || r.isKnownWithoutAlternates(domain) {
			return rawURL
		}

		alternate, hasAlternates, cacheable, err := findAlternateURL(ctx, r.client, rawURL, targetLanguage)
		if err != nil {
			return rawURL
		}
		if cacheable {
			r.storeDomain(domain, hasAlternates)
		}
		if alternate == "" {
			return rawURL
		}
		return alternate
	})
}

func (r *alternateURLReplacer) isKnownWithoutAlternates(domain string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.domains[domain]
	if !ok {
		return false
	}
	if !r.now().Before(entry.expiresAt) {
		delete(r.domains, domain)
		return false
	}
	return !entry.hasAlternates
}

func (r *alternateURLReplacer) storeDomain(domain string, hasAlternates bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.domains[domain] = alternateURLDomainCacheEntry{
		hasAlternates: hasAlternates,
		expiresAt:     r.now().Add(r.ttl),
	}
}

func alternateURLDomain(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return strings.ToLower(parsed.Hostname()), nil
}

func findAlternateURL(ctx context.Context, client *http.Client, rawURL, targetLanguage string) (alternate string, hasAlternates, cacheable bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false, false, err
	}
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", false, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", false, false, nil
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return "", false, true, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", false, false, err
	}
	alternate, hasAlternates = extractAlternateURL(string(body), targetLanguage)
	return alternate, hasAlternates, true, nil
}

func ExtractAlternateURL(html, targetLanguage string) string {
	alternate, _ := extractAlternateURL(html, targetLanguage)
	return alternate
}

func extractAlternateURL(html, targetLanguage string) (string, bool) {
	target := strings.ToLower(targetLanguage)
	hasAlternates := false
	for _, tag := range alternateLinkPattern.FindAllString(html, -1) {
		attrs := attrs(tag)
		if attrs["hreflang"] == "" {
			continue
		}
		hasAlternates = true
		if strings.EqualFold(attrs["hreflang"], target) || strings.HasPrefix(strings.ToLower(attrs["hreflang"]), strings.Split(target, "-")[0]+"-") {
			return attrs["href"], true
		}
	}
	for _, m := range ogLocalePattern.FindAllStringSubmatch(html, -1) {
		locale := m[2]
		if locale == "" {
			locale = m[3]
		}
		if strings.EqualFold(strings.ReplaceAll(locale, "_", "-"), target) {
			return "", hasAlternates
		}
	}
	return "", hasAlternates
}

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2]
	}
	return out
}
