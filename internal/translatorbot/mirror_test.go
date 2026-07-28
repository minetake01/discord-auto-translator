package translatorbot

// SPEC 3.2 message mirroring

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
func TestMirroredMessageBodyStripsGeneratedHeaders(t *testing.T) {
	input := "-# Forwarded · https://discord.com/channels/g/c/m\n> quote · [Source](https://discord.com/channels/g/c/q)\n\nbody\nsecond"
	if got, want := mirroredMessageBody(input), "body\nsecond"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// SPEC 3.2 message mirroring
func TestHandleMessageCreateForwardsAttachments(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
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
	if got := discord.sent[0].Content; got != "[en] 画像です\nhttps://cdn.discordapp.com/attachments/1/2/image.png" {
		t.Fatalf("unexpected content: %q", got)
	}
}

// SPEC 3.2 message mirroring
func TestHandleMessageCreateForwardsAttachmentOnlyMessages(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/photo.jpg?ex=1", Filename: "photo.jpg", ContentType: "image/jpeg"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("blank content should not be translated: %#v", translator.contexts)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "https://cdn.discordapp.com/attachments/1/2/photo.jpg" {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
func TestMessageContentAppendsUnsignedBareURLsForAllAttachments(t *testing.T) {
	content, err := messageContentWithAssetURLs("translated", []DiscordAttachment{
		{URL: "https://cdn.discordapp.com/attachments/1/2/image.png?ex=1&is=2&hm=3", ContentType: "image/png"},
		{URL: "https://cdn.discordapp.com/attachments/1/3/archive.zip?ex=4&is=5&hm=6", ContentType: "application/zip"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "translated\nhttps://cdn.discordapp.com/attachments/1/2/image.png\nhttps://cdn.discordapp.com/attachments/1/3/archive.zip"
	if content != want {
		t.Fatalf("got %q, want %q", content, want)
	}
}

// SPEC 3.2 message mirroring
func TestMessageContentRejectsInvalidAttachmentURL(t *testing.T) {
	_, err := messageContentWithAssetURLs("", []DiscordAttachment{{URL: "javascript:alert(1)", Filename: "bad"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid HTTP URL") {
		t.Fatalf("got %v", err)
	}
}

// SPEC 3.2 message mirroring
func TestMessageContentRejectsDiscordContentLimit(t *testing.T) {
	_, err := messageContentWithAssetURLs(strings.Repeat("a", discordMessageContentLimit), []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/a.png"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "Discord limit") {
		t.Fatalf("got %v", err)
	}
}

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
func TestHandleMessageCreateSkipsWhenTargetLinkExists(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "existing", TargetLanguage: "en",
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
func TestSendAndSaveLinkCompensatesOnDBFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	store.saveMessageLinkErr = errors.New("db unavailable")
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.sendAndSaveLink(ctx, GroupChannel{
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
	if discord.webhookDeletes[0].messageID != "sent-1" {
		t.Fatalf("unexpected delete target: %#v", discord.webhookDeletes[0])
	}
}

// SPEC 3.2 message mirroring
func TestHandleMessageCreateRateLimitBlocksTranslation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	service.SetRateLimiter(NewTokenRateLimiter(10))
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "this message should exceed the tiny rate limit",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.channelMessages) != 1 {
		t.Fatalf("channelMessages: %#v", discord.channelMessages)
	}
	notice := discord.channelMessages[0]
	if notice.channelID != "ja" || notice.replyToMessageID != "100000000000000001" {
		t.Fatalf("unexpected notification target: %#v", notice)
	}
	if !strings.Contains(notice.content, "レート制限") {
		t.Fatalf("unexpected notification: %#v", notice)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("unexpected webhook sends: %#v", discord.sent)
	}
}

// SPEC 3.2 message mirroring
func TestHandleMessageCreateFailsAllWhenTranslationFails(t *testing.T) {
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
		t.Fatal("expected aggregated error")
	}
	if len(discord.channelMessages) != 1 {
		t.Fatalf("want failure notification only, got %#v", discord.channelMessages)
	}
	notice := discord.channelMessages[0]
	if notice.channelID != "ja" || notice.replyToMessageID != "100000000000000001" {
		t.Fatalf("unexpected notification target: %#v", notice)
	}
	if !strings.Contains(notice.content, "翻訳に失敗") {
		t.Fatalf("unexpected notification: %#v", notice)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("unexpected webhook sends: %#v", discord.sent)
	}
	links, err := store.MessageTargets(ctx, "ja", "100000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("unexpected links: %#v", links)
	}
}

// SPEC 3.2 message mirroring
func TestHandleMessageUpdateSkipsUnchangedContent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
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

// SPEC 3.2 message mirroring
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
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
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
	if len(discord.webhookEdits) != 1 || discord.webhookEdits[0].content != "[en] after\nhttps://cdn.discordapp.com/attachments/1/2/image.png" {
		t.Fatalf("attachment URL not preserved in edit: %#v", discord.webhookEdits)
	}
}

// SPEC 3.2 message mirroring
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
			TargetChannelID: "en", TargetMessageID: "translated-en", TargetLanguage: "en",
			SourceContentSnapshot: "before",
		},
		{
			SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "fr", TargetMessageID: "translated-fr", TargetLanguage: "fr",
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

// SPEC 3.2 message mirroring
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

// SPEC 3.2 message mirroring
func TestMessageContentUsesStickerCDNWithoutDownload(t *testing.T) {
	content, err := messageContentWithAssetURLs("", nil, []DiscordSticker{{ID: "9", Name: "wave", FormatType: stickerFormatPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if content != "https://cdn.discordapp.com/stickers/9.png" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// SPEC 3.2 message mirroring
func TestMessageContentUsesLottiePNGCDN(t *testing.T) {
	content, err := messageContentWithAssetURLs("", nil, []DiscordSticker{{ID: "lottie-1", Name: "wave", FormatType: stickerFormatLottie}})
	if err != nil {
		t.Fatal(err)
	}
	if content != "https://cdn.discordapp.com/stickers/lottie-1.png" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// SPEC 3.2 message mirroring
func TestHandleMessageCreateReplacesDiscordMessageLink(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000009", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "linked-en", TargetLanguage: "en",
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

	want := "[en] see " + MessageJumpURL("guild", "en", "linked-en")
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}
