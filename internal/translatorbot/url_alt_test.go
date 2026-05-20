package translatorbot

import "testing"

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
