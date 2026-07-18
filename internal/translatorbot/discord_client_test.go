package translatorbot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCurrentUserIDUsesRESTBeforeGatewayOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/@me" {
			t.Fatalf("path = %q, want /users/@me", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"345678901234567890","username":"translator"}`))
	}))
	defer server.Close()

	oldEndpointUsers := discordgo.EndpointUsers
	discordgo.EndpointUsers = server.URL + "/users/"
	defer func() { discordgo.EndpointUsers = oldEndpointUsers }()

	session, err := discordgo.New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewDiscordGoAPI(session).CurrentUserID()
	if err != nil {
		t.Fatal(err)
	}
	if got != "345678901234567890" {
		t.Fatalf("CurrentUserID = %q", got)
	}
}

func TestSanitizeWebhookNameAvoidsDiscordReservedWord(t *testing.T) {
	got := sanitizeWebhookName("Discord Auto Translator")
	if strings.Contains(strings.ToLower(got), "discord") {
		t.Fatalf("sanitized name still contains reserved word: %q", got)
	}
	if got != "D-scord Auto Translator" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultWebhookNameIsProviderNeutral(t *testing.T) {
	if defaultWebhookName != "Discord Auto Translator" {
		t.Fatalf("defaultWebhookName = %q", defaultWebhookName)
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

func TestWebhookMessageURLIncludesThreadID(t *testing.T) {
	got := webhookMessageURL("wh1", "token1", "msg1", "thread1")
	if !strings.Contains(got, "thread_id=thread1") {
		t.Fatalf("got %q", got)
	}
}

func TestWebhookMessageURLOmitsQueryWithoutThreadID(t *testing.T) {
	got := webhookMessageURL("wh1", "token1", "msg1", "")
	want := discordgo.EndpointWebhookMessage("wh1", "token1", "msg1")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
