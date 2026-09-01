// Prompt-injection protection (placeholders, escaping, translatable text).
package translatorbot

import (
	"strings"
	"testing"
)

func TestNeedsTranslationIncludesExistingImageAltText(t *testing.T) {
	image := []DiscordAttachment{{Filename: "shot.png", ContentType: "image/png"}}
	withAlt := []DiscordAttachment{{Filename: "shot.png", ContentType: "image/png", Description: "出口"}}
	if needsTranslation("", nil) || needsTranslation("https://example.com", nil) || needsTranslation("", image) || needsTranslation("https://example.com", image) {
		t.Fatal("content without translatable text or existing alt should not need translation")
	}
	if !needsTranslation("hello", nil) || !needsTranslation("hello", image) || !needsTranslation("", withAlt) || !needsTranslation("https://example.com", withAlt) {
		t.Fatal("plain text or existing image alt text should need translation")
	}
}

func TestHasTranslatableText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "blank", text: " \n\t", want: false},
		{name: "URLs", text: "https://example.com/a\n<https://example.com/b>", want: false},
		{name: "Discord elements", text: "<@123> <@!456> <#789> <@&101> <:wave:202> <a:dance_1:303>", want: false},
		{name: "slash command and timestamp", text: "</ban:12345> <t:1234567890:F>", want: false},
		{name: "code", text: "`hello`\n```go\nfmt.Println(\"hello\")\n```", want: false},
		{name: "mixed protected elements", text: "<@123> https://example.com `hello` <:wave:202>", want: false},
		{name: "plain text", text: "hello", want: true},
		{name: "text with URL", text: "see https://example.com", want: true},
		{name: "Markdown link label", text: "[documentation](https://example.com)", want: true},
		{name: "unclosed code", text: "`hello", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasTranslatableText(tt.text); got != tt.want {
				t.Fatalf("hasTranslatableText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestProtectorRestoresURLsAndMarkdown(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "see https://example.com/a?x=1 and `code`"
	protected := p.Protect(in)
	if strings.Contains(protected, "https://example.com") || strings.Contains(protected, "`code`") {
		t.Fatalf("not protected: %s", protected)
	}
	if !strings.Contains(protected, "[SITE:1]") || !strings.Contains(protected, "[CODE]") {
		t.Fatalf("unexpected protected form: %s", protected)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("got %q want %q", got, in)
	}
}

func TestProtectorSiteTitleAndContext(t *testing.T) {
	p := NewProtector(NameMaps{
		Sites: map[string]string{
			"https://example.com/a": "Example Article",
			"https://example.com/b": "Example Article",
			"https://other.com/c":   "Other",
		},
	})
	p.SetSiteDescriptions(map[string]string{
		"https://example.com/a": "First page",
		"https://example.com/b": "Second page",
		"https://other.com/c":   "Other page",
	})
	p.SetSiteImages(map[string]string{
		"https://example.com/a": "https://example.com/a.jpg",
	})
	in := "see https://example.com/a and https://example.com/b and https://other.com/c"
	protected := p.Protect(in)
	want := "see [SITE:1] and [SITE:2] and [SITE:3]"
	if protected != want {
		t.Fatalf("got %q want %q", protected, want)
	}
	sites := p.SiteContext()
	if len(sites) != 3 {
		t.Fatalf("sites = %+v", sites)
	}
	if sites[0] != (SiteContextEntry{ID: "1", Title: "Example Article", Description: "First page", ImageURL: "https://example.com/a.jpg"}) {
		t.Fatalf("sites[0] = %+v", sites[0])
	}
	if sites[1] != (SiteContextEntry{ID: "2", Title: "Example Article", Description: "Second page"}) {
		t.Fatalf("sites[1] = %+v", sites[1])
	}
	if sites[2] != (SiteContextEntry{ID: "3", Title: "Other", Description: "Other page"}) {
		t.Fatalf("sites[2] = %+v", sites[2])
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorOpaqueSiteIgnoresMessyTitle(t *testing.T) {
	messy := "시노 on Instagram: \"셀프 커버는 언제나 옳다\n\n나토리 LIVE\""
	p := NewProtector(NameMaps{
		Sites: map[string]string{
			"https://www.instagram.com/p/abc/": messy,
		},
	})
	p.SetSiteDescriptions(map[string]string{
		"https://www.instagram.com/p/abc/": "caption body",
	})
	in := "good!! https://www.instagram.com/p/abc/"
	protected := p.Protect(in)
	if protected != "good!! [SITE:1]" {
		t.Fatalf("got %q", protected)
	}
	if strings.Contains(protected, "Instagram") || strings.Contains(protected, "\n") {
		t.Fatalf("title leaked into placeholder: %q", protected)
	}
	sites := p.SiteContext()
	if len(sites) != 1 || sites[0].ID != "1" || sites[0].Title != messy || sites[0].Description != "caption body" {
		t.Fatalf("sites = %+v", sites)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorRestorePrefersLongerSiteTokens(t *testing.T) {
	p := NewProtector(NameMaps{})
	var b strings.Builder
	for i := range 10 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("https://example.com/")
		b.WriteByte('a' + byte(i))
	}
	in := b.String()
	protected := p.Protect(in)
	if !strings.Contains(protected, "[SITE:10]") || !strings.Contains(protected, "[SITE:1]") {
		t.Fatalf("protected = %q", protected)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorEmojiAndMentionNames(t *testing.T) {
	p := NewProtector(NameMaps{
		Users:    map[string]string{"123": "Alice"},
		Channels: map[string]string{"789": "general"},
		Roles:    map[string]string{"101": "mod"},
	})
	in := "hi <@123> in <#789> <:wave:202> <a:dance:303>"
	protected := p.Protect(in)
	want := "hi [USER:Alice] in [CHANNEL:general] [EMOJI:wave] [EMOJI:dance]"
	if protected != want {
		t.Fatalf("got %q want %q", protected, want)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorSequentialSuffix(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "<:wave:1> <:wave:2> <t:111> <t:222>"
	protected := p.Protect(in)
	want := "[EMOJI:wave] [EMOJI:wave:2] [TIME] [TIME:2]"
	if protected != want {
		t.Fatalf("got %q want %q", protected, want)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorSlashCommandAndTimestamp(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "use </kick user:12345> at <t:1234567890:F>"
	protected := p.Protect(in)
	if !strings.Contains(protected, "[CMD:kick user]") || !strings.Contains(protected, "[TIME]") {
		t.Fatalf("unexpected protected form: %s", protected)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorDoesNotMaskSpoilers(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "||secret||"
	protected := p.Protect(in)
	if protected != in {
		t.Fatalf("spoilers should not be masked, got %q", protected)
	}
}

func TestProtectorFallbackWithoutNames(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "<@999> <#888> <@&777>"
	protected := p.Protect(in)
	want := "[USER] [CHANNEL] [ROLE]"
	if protected != want {
		t.Fatalf("got %q want %q", protected, want)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}
