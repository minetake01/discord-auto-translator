package translatorbot

import (
	"context"
	"html"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	urlPageCacheTTL            = 24 * time.Hour
	urlPageLookupConcurrency   = 4
	urlPageFetchTimeout        = 5 * time.Second
	urlPageBodyLimit           = 512 * 1024
	urlPageTitleMaxRunes       = 100
	urlPageDescriptionMaxRunes = 200
)

var (
	urlPattern          = regexp.MustCompile(`https?://[^\s<>()]+`)
	hreflangLinkPattern = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*\balternate\b[^"']*["'][^>]*>`)
	attrPattern         = regexp.MustCompile(`(?is)(href|hreflang|content|property|name)=["']([^"']+)["']`)
	metaTagPattern      = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	titleTagPattern     = regexp.MustCompile(`(?is)<title[^>]*>([^<]*)</title>`)
)

type urlPageInfo struct {
	Title       string
	Description string
	ImageURL    string
	Hreflangs   map[string]string // hreflang → href
}

type urlPageCacheEntry struct {
	info      urlPageInfo
	expiresAt time.Time
}

// urlPageCache fetches HTML once per URL and caches OGP meta plus hreflang
// alternates for both translation context and post-translation URL rewriting.
type urlPageCache struct {
	client *http.Client
	ttl    time.Duration
	now    func() time.Time

	mu    sync.Mutex
	pages map[string]urlPageCacheEntry
}

func newURLPageCache(client *http.Client, ttl time.Duration, now func() time.Time) *urlPageCache {
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &urlPageCache{
		client: client,
		ttl:    ttl,
		now:    now,
		pages:  make(map[string]urlPageCacheEntry),
	}
}

// Lookup ensures every unique HTTP(S) URL in content is cached and returns the
// page info keyed by the exact matched URL string.
func (c *urlPageCache) Lookup(ctx context.Context, content string) map[string]urlPageInfo {
	urls := uniqueURLsInText(content)
	c.ensure(ctx, urls)
	out := make(map[string]urlPageInfo, len(urls))
	for _, rawURL := range urls {
		out[rawURL] = c.cachedOrEmpty(rawURL)
	}
	return out
}

func attachURLPageMeta(tc *TranslationContext, pages map[string]urlPageInfo) {
	if len(pages) == 0 {
		return
	}
	titles := make(map[string]string, len(pages))
	descriptions := make(map[string]string, len(pages))
	images := make(map[string]string, len(pages))
	for rawURL, page := range pages {
		if title := strings.TrimSpace(page.Title); title != "" {
			titles[rawURL] = title
		}
		if desc := strings.TrimSpace(page.Description); desc != "" {
			descriptions[rawURL] = desc
		}
		if imageURL := strings.TrimSpace(page.ImageURL); imageURL != "" {
			images[rawURL] = imageURL
		}
	}
	tc.SiteTitles = titles
	tc.SiteDescriptions = descriptions
	tc.SiteImages = images
}

func (c *urlPageCache) Replace(ctx context.Context, text, targetLanguage string) string {
	matches := urlPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	type occurrence struct {
		start            int
		end              int
		replacementIndex int
	}
	occurrences := make([]occurrence, 0, len(matches))
	uniqueIndexes := make(map[string]int, len(matches))
	uniqueURLs := make([]string, 0, len(matches))
	for _, match := range matches {
		rawURL := text[match[0]:match[1]]
		index, ok := uniqueIndexes[rawURL]
		if !ok {
			index = len(uniqueURLs)
			uniqueIndexes[rawURL] = index
			uniqueURLs = append(uniqueURLs, rawURL)
		}
		occurrences = append(occurrences, occurrence{start: match[0], end: match[1], replacementIndex: index})
	}

	c.ensure(ctx, uniqueURLs)

	replacements := make([]string, len(uniqueURLs))
	for i, rawURL := range uniqueURLs {
		replacements[i] = hreflangURLForLanguage(c.cachedOrEmpty(rawURL), targetLanguage, rawURL)
	}

	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, occurrence := range occurrences {
		b.WriteString(text[last:occurrence.start])
		b.WriteString(replacements[occurrence.replacementIndex])
		last = occurrence.end
	}
	b.WriteString(text[last:])
	return b.String()
}

func hreflangURLForLanguage(info urlPageInfo, targetLanguage, fallback string) string {
	if alt := pickHreflangURL(info.Hreflangs, targetLanguage); alt != "" {
		return alt
	}
	return fallback
}

func pickHreflangURL(hreflangs map[string]string, targetLanguage string) string {
	if len(hreflangs) == 0 {
		return ""
	}
	target := strings.ToLower(targetLanguage)
	prefix := strings.Split(target, "-")[0] + "-"
	for hreflang, href := range hreflangs {
		if strings.EqualFold(hreflang, target) {
			return href
		}
	}
	for hreflang, href := range hreflangs {
		if strings.HasPrefix(strings.ToLower(hreflang), prefix) {
			return href
		}
	}
	return ""
}

func (c *urlPageCache) ensure(ctx context.Context, urls []string) {
	missing := make([]string, 0, len(urls))
	for _, rawURL := range urls {
		if c.hasFresh(rawURL) || shouldSkipURLFetch(rawURL) {
			continue
		}
		missing = append(missing, rawURL)
	}
	if len(missing) == 0 {
		return
	}

	semaphore := make(chan struct{}, urlPageLookupConcurrency)
	var wg sync.WaitGroup
	for _, rawURL := range missing {
		wg.Add(1)
		go func(rawURL string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			info, cacheable := fetchURLPage(ctx, c.client, rawURL)
			if cacheable {
				c.store(rawURL, info)
			}
		}(rawURL)
	}
	wg.Wait()
}

func (c *urlPageCache) hasFresh(rawURL string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.pages[rawURL]
	if !ok {
		return false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.pages, rawURL)
		return false
	}
	return true
}

func (c *urlPageCache) cachedOrEmpty(rawURL string) urlPageInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.pages[rawURL]
	if !ok || !c.now().Before(entry.expiresAt) {
		return urlPageInfo{}
	}
	return cloneURLPageInfo(entry.info)
}

func (c *urlPageCache) store(rawURL string, info urlPageInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pages[rawURL] = urlPageCacheEntry{
		info:      cloneURLPageInfo(info),
		expiresAt: c.now().Add(c.ttl),
	}
}

func cloneURLPageInfo(info urlPageInfo) urlPageInfo {
	out := info
	if info.Hreflangs != nil {
		out.Hreflangs = make(map[string]string, len(info.Hreflangs))
		maps.Copy(out.Hreflangs, info.Hreflangs)
	}
	return out
}

func uniqueURLsInText(text string) []string {
	matches := urlPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, rawURL := range matches {
		if _, ok := seen[rawURL]; ok {
			continue
		}
		seen[rawURL] = struct{}{}
		out = append(out, rawURL)
	}
	return out
}

func shouldSkipURLFetch(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "discord.com", "discordapp.com", "cdn.discordapp.com", "media.discordapp.net":
		return true
	}
	return strings.HasSuffix(host, ".discord.com") || strings.HasSuffix(host, ".discordapp.com")
}

func fetchURLPage(ctx context.Context, client *http.Client, rawURL string) (info urlPageInfo, cacheable bool) {
	ctx, cancel := context.WithTimeout(ctx, urlPageFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return urlPageInfo{}, false
	}
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return urlPageInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return urlPageInfo{}, false
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return urlPageInfo{}, true
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, urlPageBodyLimit))
	if err != nil {
		return urlPageInfo{}, false
	}
	info = parseURLPage(string(body))
	baseURL := parsedRequestURL(resp, rawURL)
	if info.ImageURL != "" {
		info.ImageURL = resolveMaybeRelativeURL(baseURL, info.ImageURL)
	}
	return info, true
}

func parsedRequestURL(resp *http.Response, fallback string) *url.URL {
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL
	}
	parsed, err := url.Parse(fallback)
	if err != nil {
		return nil
	}
	return parsed
}

func resolveMaybeRelativeURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || base == nil {
		return ""
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

func parseURLPage(htmlBody string) urlPageInfo {
	title, description, imageURL := extractPageMeta(htmlBody)
	return urlPageInfo{
		Title:       title,
		Description: description,
		ImageURL:    imageURL,
		Hreflangs:   extractHreflangURLs(htmlBody),
	}
}

func extractPageMeta(htmlBody string) (title, description, imageURL string) {
	var ogTitle, twitterTitle, ogDescription, twitterDescription, metaDescription, ogImage, twitterImage string
	for _, tag := range metaTagPattern.FindAllString(htmlBody, -1) {
		a := attrs(tag)
		prop := strings.ToLower(strings.TrimSpace(a["property"]))
		name := strings.ToLower(strings.TrimSpace(a["name"]))
		content := strings.TrimSpace(html.UnescapeString(a["content"]))
		if content == "" {
			continue
		}
		switch {
		case prop == "og:title" && ogTitle == "":
			ogTitle = content
		case name == "twitter:title" && twitterTitle == "":
			twitterTitle = content
		case prop == "og:description" && ogDescription == "":
			ogDescription = content
		case name == "twitter:description" && twitterDescription == "":
			twitterDescription = content
		case name == "description" && metaDescription == "":
			metaDescription = content
		case prop == "og:image" && ogImage == "":
			ogImage = content
		case name == "twitter:image" && twitterImage == "":
			twitterImage = content
		}
	}
	title = firstNonEmpty(ogTitle, twitterTitle)
	if title == "" {
		if m := titleTagPattern.FindStringSubmatch(htmlBody); len(m) == 2 {
			title = strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	description = firstNonEmpty(ogDescription, twitterDescription, metaDescription)
	imageURL = firstNonEmpty(ogImage, twitterImage)
	return truncateRunes(title, urlPageTitleMaxRunes, ""), truncateRunes(description, urlPageDescriptionMaxRunes, ""), imageURL
}

func extractHreflangURLs(htmlBody string) map[string]string {
	out := make(map[string]string)
	for _, tag := range hreflangLinkPattern.FindAllString(htmlBody, -1) {
		a := attrs(tag)
		hreflang := strings.TrimSpace(a["hreflang"])
		href := strings.TrimSpace(a["href"])
		if hreflang == "" || href == "" {
			continue
		}
		out[hreflang] = href
	}
	return out
}

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2]
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
