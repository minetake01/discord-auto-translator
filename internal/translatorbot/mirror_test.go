package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMessageSourceAllowlistAppliesToCreateAndUpdate(t *testing.T) {
	const botID = "123456789012345678"
	const webhookID = "234567890123456789"
	const otherID = "345678901234567890"
	for _, operation := range []string{"create", "update"} {
		for _, tc := range []struct {
			name       string
			message    DiscordMessage
			allowType  SourceType
			allowID    string
			allowGuild string
			want       bool
		}{
			{name: "human", message: DiscordMessage{AuthorID: "human"}, want: true},
			{name: "bot denied", message: DiscordMessage{Bot: true, AuthorID: botID}},
			{name: "bot allowed", message: DiscordMessage{Bot: true, AuthorID: botID}, allowType: SourceTypeBot, allowID: botID, allowGuild: "guild", want: true},
			{name: "bot isolated by guild", message: DiscordMessage{Bot: true, AuthorID: botID}, allowType: SourceTypeBot, allowID: botID, allowGuild: "other"},
			{name: "webhook denied even when Bot false", message: DiscordMessage{AuthorID: otherID, WebhookID: webhookID}},
			{name: "webhook allowed when Bot false", message: DiscordMessage{AuthorID: otherID, WebhookID: webhookID}, allowType: SourceTypeWebhook, allowID: webhookID, allowGuild: "guild", want: true},
			{name: "WebhookID takes priority over allowed bot author", message: DiscordMessage{Bot: true, AuthorID: botID, WebhookID: webhookID}, allowType: SourceTypeBot, allowID: botID, allowGuild: "guild"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				store := newTestStore(t)
				seedGroup(t, store)
				if tc.allowID != "" {
					if err := store.AddAllowedSource(ctx, tc.allowGuild, tc.allowType, tc.allowID, "admin"); err != nil {
						t.Fatal(err)
					}
				}
				if operation == "update" {
					if err := store.SaveMessageLink(ctx, MessageLink{
						SourceMessageID: "456789012345678901", SourceChannelID: "ja", GroupID: "g",
						TargetChannelID: "en", TargetMessageID: "567890123456789012", TargetLanguage: "en",
						SourceAuthorID: tc.message.AuthorID, SourceContentSnapshot: "before",
					}); err != nil {
						t.Fatal(err)
					}
				}
				discord := &fakeDiscordAPI{}
				translator := &echoTranslator{}
				service := NewService(store, discord, translator)
				tc.message.ID = "456789012345678901"
				tc.message.ChannelID = "ja"
				tc.message.GuildID = "guild"
				tc.message.AuthorDisplayName = "source"
				tc.message.Content = "after"
				var err error
				if operation == "create" {
					err = service.HandleMessageCreate(ctx, tc.message)
				} else {
					err = service.HandleMessageUpdate(ctx, tc.message)
				}
				if err != nil {
					t.Fatal(err)
				}
				processed := len(translator.contexts) == 1
				if processed != tc.want {
					t.Fatalf("processed = %v, want %v; sends=%d edits=%d", processed, tc.want, len(discord.sent), len(discord.webhookEdits))
				}
			})
		}
	}
}

func TestMessageSourcePolicyAlwaysExcludesSelfAndManagedWebhooks(t *testing.T) {
	ctx := context.Background()
	const selfID = "123456789012345678"
	const managedWebhookID = "234567890123456789"
	for _, operation := range []string{"create", "update"} {
		for _, tc := range []struct {
			name    string
			message DiscordMessage
		}{
			{name: "native self message", message: DiscordMessage{Bot: true, AuthorID: selfID}},
			{name: "managed output webhook", message: DiscordMessage{AuthorID: "345678901234567890", WebhookID: managedWebhookID}},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				store := newTestStore(t)
				seedGroup(t, store)
				if _, err := store.db.ExecContext(ctx, `UPDATE group_channels SET webhook_id=? WHERE guild_id='guild' AND channel_id='ja'`, managedWebhookID); err != nil {
					t.Fatal(err)
				}
				if err := store.AddAllowedSource(ctx, "guild", SourceTypeBot, selfID, "admin"); err != nil {
					t.Fatal(err)
				}
				if operation == "update" {
					if err := store.SaveMessageLink(ctx, MessageLink{SourceMessageID: "456789012345678901", SourceChannelID: "ja", GroupID: "g", TargetChannelID: "en", TargetMessageID: "567890123456789012", TargetLanguage: "en", SourceContentSnapshot: "before"}); err != nil {
						t.Fatal(err)
					}
				}
				discord := &fakeDiscordAPI{}
				translator := &echoTranslator{}
				service := NewService(store, discord, translator)
				service.SetSelfBotUserID(selfID)
				tc.message.ID, tc.message.ChannelID, tc.message.GuildID, tc.message.Content = "456789012345678901", "ja", "guild", "after"
				var err error
				if operation == "create" {
					err = service.HandleMessageCreate(ctx, tc.message)
				} else {
					err = service.HandleMessageUpdate(ctx, tc.message)
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(translator.contexts) != 0 || len(discord.sent) != 0 || len(discord.webhookEdits) != 0 {
					t.Fatalf("excluded source was processed")
				}
			})
		}
	}
}

func TestMessageSourcePolicyFailsClosedOnDatabaseErrors(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"create", "update"} {
		for _, message := range []DiscordMessage{
			{Bot: true, AuthorID: "123456789012345678"},
			{AuthorID: "234567890123456789", WebhookID: "345678901234567890"},
		} {
			t.Run(operation, func(t *testing.T) {
				store := newTestStore(t)
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				discord := &fakeDiscordAPI{}
				translator := &echoTranslator{}
				service := NewService(store, discord, translator)
				message.ID, message.ChannelID, message.GuildID, message.Content = "456789012345678901", "ja", "guild", "after"
				var err error
				if operation == "create" {
					err = service.HandleMessageCreate(ctx, message)
				} else {
					err = service.HandleMessageUpdate(ctx, message)
				}
				if err == nil || !strings.Contains(err.Error(), "message source policy") {
					t.Fatalf("error = %v", err)
				}
				if len(translator.contexts) != 0 || len(discord.sent) != 0 || len(discord.webhookEdits) != 0 {
					t.Fatal("DB policy failure translated an automated source")
				}
			})
		}
	}

	store := newTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	allowed, err := NewService(store, &fakeDiscordAPI{}, &echoTranslator{}).shouldProcessMessage(ctx, DiscordMessage{AuthorID: "human"})
	if err != nil || !allowed {
		t.Fatalf("human policy = allowed %v, error %v", allowed, err)
	}
}

func TestMirroredMessageBodyStripsGeneratedHeaders(t *testing.T) {
	input := "-# Forwarded · https://discord.com/channels/g/c/m\n> quote · [Source](https://discord.com/channels/g/c/q)\n\nbody\nsecond"
	if got, want := mirroredMessageBody(input), "body\nsecond"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHandleMessageCreateForwardsAttachments(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	stubImageHTTP(service)
	seedGroup(t, store)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "画像です",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/image.png?ex=1&is=2&hm=3", Filename: "image.png", ContentType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if got := discord.sent[0].Content; got != "[en] 画像です" {
		t.Fatalf("unexpected content: %q", got)
	}
	if len(discord.sent[0].Files) != 1 || discord.sent[0].Files[0].Name != "image.png" || discord.sent[0].Files[0].Description != "" {
		t.Fatalf("unexpected files: %#v", discord.sent[0].Files)
	}
}

func TestHandleMessageCreateReuploadsAllImagesAndCapsVision(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	stubImageHTTP(service)
	seedGroup(t, store)
	attachments := make([]DiscordAttachment, 5)
	for i := range attachments {
		attachments[i] = DiscordAttachment{
			URL:         fmt.Sprintf("https://cdn.discordapp.com/%d.png", i),
			Filename:    fmt.Sprintf("%d.png", i),
			ContentType: "image/png",
		}
	}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "画像です",
		Attachments: attachments,
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if len(discord.sent[0].Files) != 5 {
		t.Fatalf("files = %d, want 5: %#v", len(discord.sent[0].Files), discord.sent[0].Files)
	}
	if len(translator.visions) != 1 || len(translator.visions[0]) != visionMaxImages {
		t.Fatalf("vision images = %#v", translator.visions)
	}
}

func TestHandleMessageCreateLinksImageThatCouldNotBeFetched(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	service.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody}, nil
	})}
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "画像です",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/image.png?ex=1&is=2&hm=3", Filename: "image.png", ContentType: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if got := discord.sent[0].Content; got != "[en] 画像です\nhttps://cdn.discordapp.com/attachments/1/2/image.png" {
		t.Fatalf("content = %q", got)
	}
	if len(discord.sent[0].Files) != 0 {
		t.Fatalf("failed image fetch must not reupload: %#v", discord.sent[0].Files)
	}
}

func TestHandleMessageCreateTranslatesExistingImageAltText(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	stubImageHTTP(service)
	seedGroup(t, store)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "見て",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/a.png", Filename: "a.png", ContentType: "image/png", Description: "出口はこちら"}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "[en] 見て" {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if got := mirroredImageDescription(discord); got != "[en] 出口はこちら" {
		t.Fatalf("description = %q files=%#v edits=%#v", got, discord.sent[0].Files, discord.webhookAttachmentEdits)
	}
}

func TestHandleMessageCreateForwardsAttachmentOnlyMessages(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	stubImageHTTP(service)
	seedGroup(t, store)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/photo.jpg?ex=1", Filename: "photo.jpg", ContentType: "image/jpeg"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("image-only messages without alt are not translation targets: %#v", translator.contexts)
	}
	if len(translator.altContexts) != 0 {
		t.Fatalf("image-only messages without alt must not translate alts: %#v", translator.altContexts)
	}
	if len(discord.sent) != 1 || strings.TrimSpace(discord.sent[0].Content) != "" {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if len(discord.sent[0].Files) != 1 || discord.sent[0].Files[0].Name != "photo.jpg" || discord.sent[0].Files[0].Description != "" {
		t.Fatalf("unexpected files: %#v", discord.sent[0].Files)
	}
}

func TestHandleMessageCreateTranslatesImageOnlyExistingAltText(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	stubImageHTTP(service)
	seedGroup(t, store)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/a.png", Filename: "a.png", ContentType: "image/png", Description: "出口はこちら"}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 0 {
		t.Fatalf("image-only messages with alt must not call body translation: %#v", translator.contexts)
	}
	if len(translator.altContexts) != 1 {
		t.Fatalf("image-only messages with alt are alt translation targets: %#v", translator.altContexts)
	}
	if len(discord.sent) != 1 || strings.TrimSpace(discord.sent[0].Content) != "" {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if got := mirroredImageDescription(discord); got != "[en] 出口はこちら" {
		t.Fatalf("description = %q files=%#v edits=%#v", got, discord.sent[0].Files, discord.webhookAttachmentEdits)
	}
}

func TestHandleMessageCreatePatchesTranslatedAltAfterPosting(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	altStarted := make(chan struct{})
	altRelease := make(chan struct{})
	translator := &delayedAltTranslator{altStarted: altStarted, altRelease: altRelease}
	service := NewService(store, discord, translator)
	stubImageHTTP(service)
	seedGroup(t, store)

	done := make(chan error, 1)
	go func() {
		done <- service.HandleMessageCreate(ctx, DiscordMessage{
			ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "見て",
			Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/a.png", Filename: "a.png", ContentType: "image/png", Description: "出口はこちら"}},
		})
	}()

	select {
	case <-altStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("alt translation did not start")
	}

	deadline := time.Now().Add(2 * time.Second)
	var sent []WebhookSend
	for time.Now().Before(deadline) {
		sent = discord.webhookSends()
		if len(sent) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(sent) != 1 || sent[0].Content != "[en] 見て" {
		t.Fatalf("sent before alt finished: %#v", sent)
	}
	if len(sent[0].Files) != 1 || sent[0].Files[0].Description != "出口はこちら" {
		t.Fatalf("first send must keep source alt: %#v", sent[0].Files)
	}
	if edits := discord.attachmentEdits(); len(edits) != 0 {
		t.Fatalf("patch before alt finished: %#v", edits)
	}

	close(altRelease)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HandleMessageCreate did not finish")
	}

	edits := discord.attachmentEdits()
	if len(edits) != 1 || len(edits[0].attachments) != 1 || edits[0].attachments[0].Description != "[en] 出口はこちら" {
		t.Fatalf("attachment edits: %#v", edits)
	}
	if edits[0].attachments[0].ID == "" {
		t.Fatal("patch must include the posted attachment id")
	}
}

func TestHandleMessageCreateKeepsSourceAltWhenAltTranslationFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &failingAltTranslator{err: errors.New("alt failed")})
	stubImageHTTP(service)
	seedGroup(t, store)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "見て",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/a.png", Filename: "a.png", ContentType: "image/png", Description: "出口はこちら"}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "[en] 見て" {
		t.Fatalf("body must still post: %#v", discord.sent)
	}
	if got := mirroredImageDescription(discord); got != "出口はこちら" {
		t.Fatalf("source alt must remain: %q edits=%#v", got, discord.webhookAttachmentEdits)
	}
}

func TestHandleMessageCreateSkipsEmptyMessageWithoutPoll(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000051", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("empty message should be skipped: %#v", discord.sent)
	}
}

func TestHandleMessageCreateSkipsTranslationForURLOnlyContentAndRewritesHreflang(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<link rel="alternate" hreflang="en" href="https://example.com/en">`)
	}))
	t.Cleanup(page.Close)
	service.urlPages.client = page.Client()
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: page.URL,
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("URL-only content should not be translated: %#v", translator.contexts)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "https://example.com/en" {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

func TestHandleMessageCreateSkipsTranslationForDigitOnly(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "123",
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("digit-only content should not be translated: %#v", translator.contexts)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "123" {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

func TestHandleMessageCreateSkipsTranslationForUnicodeEmojiOnly(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "👍 🎉",
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("unicode-emoji-only content should not be translated: %#v", translator.contexts)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "👍 🎉" {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

func TestHandleMessageCreateSkipsTranslationForStickerOnly(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Stickers: []DiscordSticker{{ID: "9", Name: "wave", FormatType: stickerFormatPNG}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 0 {
		t.Fatalf("sticker-only messages should not be translated: %#v", translator.contexts)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "https://cdn.discordapp.com/stickers/9.png" || len(discord.sent[0].Files) != 0 {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

func TestHandleMessageCreateTranslatesMarkdownLinkLabel(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "[資料](https://example.invalid)",
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("Markdown link label should be translated: %#v", translator.contexts)
	}
}

func TestMessageContentAppendsUnsignedBareURLsForAllAttachments(t *testing.T) {
	content, err := messageContentWithAssetURLs("translated", []DiscordAttachment{
		{URL: "https://cdn.discordapp.com/attachments/1/2/image.png?ex=1&is=2&hm=3", ContentType: "image/png"},
		{URL: "https://cdn.discordapp.com/attachments/1/3/archive.zip?ex=4&is=5&hm=6", ContentType: "application/zip"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "translated\nhttps://cdn.discordapp.com/attachments/1/3/archive.zip"
	if content != want {
		t.Fatalf("got %q, want %q", content, want)
	}
}

func TestMessageContentRejectsInvalidAttachmentURL(t *testing.T) {
	_, err := messageContentWithAssetURLs("", []DiscordAttachment{{URL: "javascript:alert(1)", Filename: "bad"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid HTTP URL") {
		t.Fatalf("got %v", err)
	}
}

func TestMessageContentAllowsTextOverTheDiscordLimit(t *testing.T) {
	content, err := messageContentWithAssetURLs(strings.Repeat("a", discordMessageContentLimit), []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/a.zip", ContentType: "application/zip"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(content, "\nhttps://cdn.discordapp.com/attachments/1/2/a.zip") {
		t.Fatalf("asset URL missing from content: %q", content)
	}
	if discordContentLength(content) <= discordMessageContentLimit {
		t.Fatalf("fixture should exceed the Discord limit: %d", discordContentLength(content))
	}
}

func TestHandleMessageUpdateInThreadPassesThreadIDToWebhookEdit(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		channelNames: map[string]string{"ja": "announcements-ja", "100000000000000005": "topic"},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveThreadLink(ctx, ThreadLink{GroupID: "g", SourceThreadID: "100000000000000005", SourceChannelID: "ja", TargetThreadID: "thread-en", TargetChannelID: "en", TargetLanguage: "en"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "100000000000000005", GroupID: "g",
		TargetChannelID: "thread-en", TargetMessageID: "100000000000000015", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{ID: "100000000000000006", ChannelID: "100000000000000005", GuildID: "guild", AuthorID: "u", ThreadName: "topic", Content: "after"}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if got := translator.contexts[0]; got.ChannelName != "announcements-ja" || got.ThreadName != "topic" {
		t.Fatalf("unexpected translation context: %#v", got)
	}

	if len(discord.webhookEdits) != 1 {
		t.Fatalf("webhook edits: %#v", discord.webhookEdits)
	}
	if got := discord.webhookEdits[0]; got.messageID != "100000000000000015" || got.threadID != "thread-en" || got.content != "[en] after" {
		t.Fatalf("unexpected webhook edit: %#v", got)
	}
}

func TestHandleMessageUpdateKeepsHreflangRewrite(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<link rel="alternate" hreflang="en" href="https://example.com/en">`)
	}))
	t.Cleanup(page.Close)
	service.urlPages.client = page.Client()
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000015", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000006", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "see " + page.URL,
	}); err != nil {
		t.Fatal(err)
	}

	if len(discord.webhookEdits) != 1 {
		t.Fatalf("webhook edits: %#v", discord.webhookEdits)
	}
	if got := discord.webhookEdits[0].content; got != "[en] see https://example.com/en" {
		t.Fatalf("got %q", got)
	}
}

func TestHandleMessageUpdateSkipsTranslationForURLOnlyContent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<link rel="alternate" hreflang="en" href="https://example.com/en">`)
	}))
	t.Cleanup(page.Close)
	service.urlPages.client = page.Client()
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000015", TargetLanguage: "en", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000006", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: page.URL,
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("URL-only edit should not be translated: %#v", translator.contexts)
	}
	if len(discord.webhookEdits) != 1 || discord.webhookEdits[0].content != "https://example.com/en" {
		t.Fatalf("edits: %#v", discord.webhookEdits)
	}
	links, err := store.MessageTargets(ctx, "ja", "100000000000000006")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].SourceContentSnapshot != page.URL {
		t.Fatalf("snapshot not updated: %#v", links)
	}
}

func TestHandleMessageDeleteInThreadPassesThreadIDToWebhookDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveThreadLink(ctx, ThreadLink{GroupID: "g", SourceThreadID: "100000000000000005", SourceChannelID: "ja", TargetThreadID: "thread-en", TargetChannelID: "en", TargetLanguage: "en"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "100000000000000005", GroupID: "g",
		TargetChannelID: "thread-en", TargetMessageID: "100000000000000015", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageDelete(ctx, "guild", "100000000000000005", "100000000000000006"); err != nil {
		t.Fatal(err)
	}

	if len(discord.webhookDeletes) != 1 {
		t.Fatalf("webhook deletes: %#v", discord.webhookDeletes)
	}
	if got := discord.webhookDeletes[0]; got.messageID != "100000000000000015" || got.threadID != "thread-en" {
		t.Fatalf("unexpected webhook delete: %#v", got)
	}
	links, err := store.MessageTargets(ctx, "100000000000000005", "100000000000000006")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("message links were not deleted: %#v", links)
	}
}

func TestHandleMessageCreateSkipsThreadSystemMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "thread-system", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "議題", ThreadSystemMessage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("thread system message was translated: %#v", discord.sent)
	}
}

func TestHandleMessageCreateSkipsWhenTargetLinkExists(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000013", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "hello",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("expected no webhook send when link exists, got %#v", discord.sent)
	}
}

func TestHandleMessageCreateDuplicateDelivery(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	msg := DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "hello",
	}
	if err := service.HandleMessageCreate(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleMessageCreate(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("duplicate delivery sent %d messages, want 1: %#v", len(discord.sent), discord.sent)
	}
}

func TestSendAndSaveLinkCompensatesOnDBFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	store.saveMessageLinkErr = errors.New("db unavailable")
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	_, err := service.sendAndSaveLink(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}, "", WebhookSend{Content: "[en] hello", Username: "u"}, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "hello",
	}, MessageReference{})
	if err == nil {
		t.Fatal("expected save error")
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if len(discord.webhookDeletes) != 1 {
		t.Fatalf("expected compensating delete, got %#v", discord.webhookDeletes)
	}
	if discord.webhookDeletes[0].messageID != "900000000000000001" {
		t.Fatalf("unexpected delete target: %#v", discord.webhookDeletes[0])
	}
}

const rateLimitOversizeContent = "this message should exceed the tiny rate limit"

func TestHandleMessageCreateRateLimitMirrorsUntranslated(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	service.SetRateLimiter(NewRateLimiter(10))
	seedGroup(t, store)

	message := DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: rateLimitOversizeContent,
	}
	err := service.HandleMessageCreate(ctx, message)
	if !errors.Is(err, errTranslationRateLimited) {
		t.Fatalf("error = %v, want the rate-limit error", err)
	}
	want := untranslatedMirrorContent("en", rateLimitOversizeContent)
	if got := sentContents(discord); len(got) != 1 || got[0] != want {
		t.Fatalf("sent = %q, want %q", got, want)
	}

	// The fallback is a recorded mirror, so a duplicate delivery must not post
	// it a second time.
	if err := service.HandleMessageCreate(ctx, message); err != nil {
		t.Fatal(err)
	}
	if got := sentContents(discord); len(got) != 1 {
		t.Fatalf("duplicate delivery posted the fallback again: %q", got)
	}
}

func TestHandleMessageCreateMirrorsUntranslatedWhenTranslationFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &selectiveFailTranslator{failLanguage: "en"})
	seedThreeChannelGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "hello",
	})
	if err == nil {
		t.Fatal("expected the translation error")
	}
	sent := sentContents(discord)
	want := []string{untranslatedMirrorContent("en", "hello"), untranslatedMirrorContent("fr", "hello")}
	if len(sent) != len(want) {
		t.Fatalf("sent = %q, want one fallback per destination", sent)
	}
	for _, content := range want {
		if !slices.Contains(sent, content) {
			t.Fatalf("sent = %q, missing %q", sent, content)
		}
	}
	links, err := store.MessageTargets(ctx, "ja", "100000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %#v, want one fallback mirror per destination", links)
	}
	for _, link := range links {
		if link.SourceContentSnapshot != "hello" {
			t.Fatalf("link snapshot = %q, want the original text", link.SourceContentSnapshot)
		}
	}
}

func postSourceMessage(t *testing.T, service *Service, channelID, messageID, content string) error {
	t.Helper()
	return service.HandleMessageCreate(context.Background(), DiscordMessage{
		ID: messageID, ChannelID: channelID, GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: content,
	})
}

// untranslatedMirrorContent is what a destination receives when translation
// fails: the original text, a newline, and the localized failure note.
func untranslatedMirrorContent(language, source string) string {
	return source + "\n-# " + localizedUIString(language, uiKeyTranslationFailed)
}

func TestHandleMessageCreateMirrorsUntranslatedDuringProviderOutage(t *testing.T) {
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &stickyErrorTranslator{err: errTranslationProvider}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	for _, message := range []struct {
		channelID, messageID, content string
	}{
		{"ja", "100000000000000001", "hello"},
		{"ja", "100000000000000002", "again"},
		{"en", "100000000000000003", "from en"},
	} {
		if err := postSourceMessage(t, service, message.channelID, message.messageID, message.content); !errors.Is(err, errTranslationProvider) {
			t.Fatalf("error = %v, want the provider error", err)
		}
	}
	want := []string{
		untranslatedMirrorContent("en", "hello"),
		untranslatedMirrorContent("en", "again"),
		untranslatedMirrorContent("ja", "from en"),
	}
	if got := sentContents(discord); !slices.Equal(got, want) {
		t.Fatalf("sent = %q, want %q", got, want)
	}
}

func TestHandleMessageUpdateReplacesUntranslatedMirror(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &stickyErrorTranslator{err: errors.New("translation failed")}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	if err := postSourceMessage(t, service, "ja", "100000000000000001", "hello"); err == nil {
		t.Fatal("expected the translation error")
	}
	translator.setErr(nil)
	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "hello edited",
	}); err != nil {
		t.Fatal(err)
	}
	if edits := discord.webhookEdits; len(edits) != 1 || edits[0].content != "[en] hello edited" {
		t.Fatalf("edits = %#v, want the retranslated mirror", edits)
	}
}

func TestHandleMessageUpdateSkipsUnchangedContent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000015", TargetLanguage: "en",
		SourceContentSnapshot: "same",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "same",
	}); err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 0 || len(discord.webhookEdits) != 0 {
		t.Fatalf("unexpected translation/edit: contexts=%#v edits=%#v", translator.contexts, discord.webhookEdits)
	}
}

func TestHandleMessageUpdateUpdatesSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000015", TargetLanguage: "en",
		SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "after",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/image.png?ex=1&hm=2"}},
	}); err != nil {
		t.Fatal(err)
	}
	links, err := store.MessageTargets(ctx, "ja", "100000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].SourceContentSnapshot != "after" {
		t.Fatalf("snapshot not updated: %#v", links)
	}
	if len(discord.webhookEdits) != 1 || discord.webhookEdits[0].content != "[en] after" {
		t.Fatalf("image URLs should not be appended on edit: %#v", discord.webhookEdits)
	}
}

func TestHandleMessageUpdateBatchesTranslationByGroup(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedMultiLangGroup(t, store)
	for _, link := range []MessageLink{
		{
			SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "700000000000000016", TargetLanguage: "en",
			SourceContentSnapshot: "before",
		},
		{
			SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "fr", TargetMessageID: "700000000000000017", TargetLanguage: "fr",
			SourceContentSnapshot: "before",
		},
	} {
		if err := store.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "after",
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("expected one batched translation call, got %#v", translator.contexts)
	}
	if len(discord.webhookEdits) != 2 {
		t.Fatalf("expected two webhook edits, got %#v", discord.webhookEdits)
	}
}

func TestHandleMessageCreateForwardsTTS(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "こんにちは", TTS: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || !discord.sent[0].TTS {
		t.Fatalf("expected TTS webhook send, got %#v", discord.sent)
	}
}

func TestMessageContentUsesStickerCDNWithoutDownload(t *testing.T) {
	content, err := messageContentWithAssetURLs("", nil, []DiscordSticker{{ID: "9", Name: "wave", FormatType: stickerFormatPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if content != "https://cdn.discordapp.com/stickers/9.png" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestMessageContentUsesLottiePNGCDN(t *testing.T) {
	content, err := messageContentWithAssetURLs("", nil, []DiscordSticker{{ID: "lottie-1", Name: "wave", FormatType: stickerFormatLottie}})
	if err != nil {
		t.Fatal(err)
	}
	if content != "https://cdn.discordapp.com/stickers/lottie-1.png" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestHandleMessageCreateReplacesDiscordMessageLink(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000009", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000014", TargetLanguage: "en",
		SourceAuthorID: "author", SourceContentSnapshot: "referenced",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u",
		Content:           "see " + MessageJumpURL("guild", "ja", "100000000000000009"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "[en] see " + MessageJumpURL("guild", "en", "700000000000000014")
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}
