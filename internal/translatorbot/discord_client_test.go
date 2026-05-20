package translatorbot

import (
	"strings"
	"testing"
)

func TestSanitizeWebhookNameAvoidsDiscordReservedWord(t *testing.T) {
	got := sanitizeWebhookName("Discord Gemini Auto Translator")
	if strings.Contains(strings.ToLower(got), "discord") {
		t.Fatalf("sanitized name still contains reserved word: %q", got)
	}
	if got != "D-scord Gemini Auto Translator" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeWebhookNameUsesFallbackForBlankNames(t *testing.T) {
	if got := sanitizeWebhookName("   "); got != defaultWebhookName {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeWebhookNameLimitsLength(t *testing.T) {
	got := sanitizeWebhookName(strings.Repeat("あ", 81))
	if len([]rune(got)) != 80 {
		t.Fatalf("got %d runes, want 80", len([]rune(got)))
	}
}

func TestSanitizeWebhookAvatarURLAllowsHTTPURLs(t *testing.T) {
	got := sanitizeWebhookAvatarURL("https://cdn.discordapp.com/avatar.png")
	if got != "https://cdn.discordapp.com/avatar.png" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeWebhookAvatarURLRejectsDataURL(t *testing.T) {
	got := sanitizeWebhookAvatarURL("data:image/png;base64,AAAA")
	if got != "" {
		t.Fatalf("got %q, want blank avatar URL", got)
	}
}

func TestSanitizeWebhookAvatarURLRejectsLongURL(t *testing.T) {
	got := sanitizeWebhookAvatarURL("https://example.com/" + strings.Repeat("a", 2048))
	if got != "" {
		t.Fatalf("got %q, want blank avatar URL", got)
	}
}
