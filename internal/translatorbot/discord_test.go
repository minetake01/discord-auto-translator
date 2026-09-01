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

// Discord forbids webhook names containing "discord".
func TestSanitizeWebhookNameAvoidsDiscordReservedWord(t *testing.T) {
	got := sanitizeWebhookName("Discord Auto Translator")
	if got == "" {
		t.Fatal("sanitized name must not be empty")
	}
	if strings.Contains(strings.ToLower(got), "discord") {
		t.Fatalf("sanitized name still contains reserved word: %q", got)
	}
}

// Blank names fall back to a non-empty safe webhook name.
func TestSanitizeWebhookNameUsesFallbackForBlankNames(t *testing.T) {
	got := sanitizeWebhookName("   ")
	if got == "" {
		t.Fatal("blank name must fall back to a non-empty webhook name")
	}
	if strings.Contains(strings.ToLower(got), "discord") {
		t.Fatalf("fallback still contains reserved word: %q", got)
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

func TestDiscordFilesAndMetaIncludesDescription(t *testing.T) {
	files, attachments, err := discordFilesAndMeta([]WebhookFile{{
		Name:        "sign.png",
		ContentType: "image/png",
		Description: "Exit this way",
		Data:        png1x1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "sign.png" || files[0].ContentType != "image/png" {
		t.Fatalf("files = %#v", files)
	}
	if len(attachments) != 1 || attachments[0].ID != "0" || attachments[0].Filename != "sign.png" || attachments[0].Description != "Exit this way" {
		t.Fatalf("attachments = %#v", attachments)
	}
}

func TestFetchedMessageFromDiscordIncludesEmbedTitleForQuotes(t *testing.T) {
	got := fetchedMessageFromDiscord(&discordgo.Message{
		Content: "> -# A poll has started. · [Vote](https://discord.com/channels/g/c/m)",
		Embeds:  []*discordgo.MessageEmbed{{Title: "Favorite color?", Description: "1. Red"}},
	})
	want := "> -# A poll has started. · [Vote](https://discord.com/channels/g/c/m)\nFavorite color?"
	if got.Content != want {
		t.Fatalf("Content = %q, want %q", got.Content, want)
	}
}
