package translatorbot

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var urlPattern = regexp.MustCompile(`https?://[^\s<>()]+`)
var alternateLinkPattern = regexp.MustCompile(`(?is)<link\s+[^>]*rel=["'][^"']*\balternate\b[^"']*["'][^>]*>`)
var attrPattern = regexp.MustCompile(`(?is)(href|hreflang|content|property)=["']([^"']+)["']`)
var ogLocalePattern = regexp.MustCompile(`(?is)<meta\s+[^>]*(property=["']og:locale:alternate["'][^>]*content=["']([^"']+)["']|content=["']([^"']+)["'][^>]*property=["']og:locale:alternate["'])[^>]*>`)

func ReplaceAlternateURLs(ctx context.Context, text, targetLanguage string, client *http.Client) string {
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	return urlPattern.ReplaceAllStringFunc(text, func(u string) string {
		alt, err := FindAlternateURL(ctx, client, u, targetLanguage)
		if err != nil || alt == "" {
			return u
		}
		return alt
	})
}

func FindAlternateURL(ctx context.Context, client *http.Client, rawURL, targetLanguage string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return "", nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}
	return ExtractAlternateURL(string(body), targetLanguage), nil
}

func ExtractAlternateURL(html, targetLanguage string) string {
	target := strings.ToLower(targetLanguage)
	for _, tag := range alternateLinkPattern.FindAllString(html, -1) {
		attrs := attrs(tag)
		if strings.EqualFold(attrs["hreflang"], target) || strings.HasPrefix(strings.ToLower(attrs["hreflang"]), strings.Split(target, "-")[0]+"-") {
			return attrs["href"]
		}
	}
	for _, m := range ogLocalePattern.FindAllStringSubmatch(html, -1) {
		locale := m[2]
		if locale == "" {
			locale = m[3]
		}
		if strings.EqualFold(strings.ReplaceAll(locale, "_", "-"), target) {
			return ""
		}
	}
	return ""
}

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2]
	}
	return out
}
