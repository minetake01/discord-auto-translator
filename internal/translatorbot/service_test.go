package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type fakeDiscordAPI struct {
	sent              []WebhookSend
	reactions         []reactionCall
	removedReactions  []reactionCall
	threads           []threadCall
	webhookEdits      []webhookEditCall
	webhookDeletes    []webhookDeleteCall
	pinCalls          []pinCall
	edits             []threadEditCall
	deletes           []string
	guildNames        map[string]string
	guildDescriptions map[string]string
	channelNames      map[string]string
	channelTopics     map[string]string
	messageContents   map[string]string
	messageErrors     map[string]error
	nextID            int
}

type reactionCall struct {
	channelID string
	messageID string
	emoji     string
}

type threadCall struct {
	channelID   string
	channelType int
	messageID   string
	name        string
	content     string
}

type threadEditCall struct {
	threadID string
	name     string
}

type webhookEditCall struct {
	messageID string
	threadID  string
	content   string
}

type webhookDeleteCall struct {
	messageID string
	threadID  string
}

type pinCall struct {
	channelID string
	messageID string
	pinned    bool
}

func (f *fakeDiscordAPI) GuildName(guildID string) (string, error) {
	return f.guildNames[guildID], nil
}

func (f *fakeDiscordAPI) GuildDescription(guildID string) (string, error) {
	return f.guildDescriptions[guildID], nil
}

func (f *fakeDiscordAPI) ChannelName(channelID string) (string, error) {
	return f.channelNames[channelID], nil
}

func (f *fakeDiscordAPI) ChannelTopic(channelID string) (string, error) {
	return f.channelTopics[channelID], nil
}

func (f *fakeDiscordAPI) MessageContent(channelID, messageID string) (string, error) {
	key := channelID + "\x00" + messageID
	if err := f.messageErrors[key]; err != nil {
		return "", err
	}
	content, ok := f.messageContents[key]
	if !ok {
		return "", errors.New("message not found")
	}
	return content, nil
}

func (f *fakeDiscordAPI) CreateWebhook(channelID, name string) (id, token string, err error) {
	return "webhook-" + channelID, "token-" + channelID, nil
}

func (f *fakeDiscordAPI) SendChannelMessage(channelID, content string) error {
	f.nextID++
	f.sent = append(f.sent, WebhookSend{Content: content})
	return nil
}

func (f *fakeDiscordAPI) SendWebhook(webhookID, token string, msg WebhookSend) (messageID string, err error) {
	f.nextID++
	f.sent = append(f.sent, msg)
	return fmt.Sprintf("sent-%d", f.nextID), nil
}

func (f *fakeDiscordAPI) EditWebhook(webhookID, token, messageID, threadID, content string) error {
	f.webhookEdits = append(f.webhookEdits, webhookEditCall{messageID: messageID, threadID: threadID, content: content})
	return nil
}

func (f *fakeDiscordAPI) DeleteWebhook(webhookID, token, messageID, threadID string) error {
	f.webhookDeletes = append(f.webhookDeletes, webhookDeleteCall{messageID: messageID, threadID: threadID})
	return nil
}

func (f *fakeDiscordAPI) AddReaction(channelID, messageID, emoji string) error {
	f.reactions = append(f.reactions, reactionCall{channelID: channelID, messageID: messageID, emoji: emoji})
	return nil
}

func (f *fakeDiscordAPI) RemoveOwnReaction(channelID, messageID, emoji string) error {
	f.removedReactions = append(f.removedReactions, reactionCall{channelID: channelID, messageID: messageID, emoji: emoji})
	return nil
}

func (f *fakeDiscordAPI) PinMessage(channelID, messageID string) error {
	f.pinCalls = append(f.pinCalls, pinCall{channelID: channelID, messageID: messageID, pinned: true})
	return nil
}

func (f *fakeDiscordAPI) UnpinMessage(channelID, messageID string) error {
	f.pinCalls = append(f.pinCalls, pinCall{channelID: channelID, messageID: messageID, pinned: false})
	return nil
}

func (f *fakeDiscordAPI) CreateThread(channelID string, channelType int, name, initialMessage string) (threadID, initialMessageID string, err error) {
	f.nextID++
	threadID = fmt.Sprintf("thread-%d", f.nextID)
	if isThreadOnlyChannelType(channelType) {
		initialMessageID = threadID
	}
	f.threads = append(f.threads, threadCall{channelID: channelID, channelType: channelType, name: name, content: initialMessage})
	return threadID, initialMessageID, nil
}

func (f *fakeDiscordAPI) CreateThreadFromMessage(channelID, messageID, name string) (threadID string, err error) {
	f.nextID++
	f.threads = append(f.threads, threadCall{channelID: channelID, messageID: messageID, name: name})
	return fmt.Sprintf("thread-%d", f.nextID), nil
}

func (f *fakeDiscordAPI) EditThread(threadID, name string) error {
	f.edits = append(f.edits, threadEditCall{threadID: threadID, name: name})
	return nil
}

func (f *fakeDiscordAPI) DeleteThread(threadID string) error {
	f.deletes = append(f.deletes, threadID)
	return nil
}

type echoTranslator struct {
	contexts []TranslationContext
}

func (e *echoTranslator) TranslateMulti(ctx context.Context, targetLanguages []string, text string, translationContext TranslationContext, glossary []GlossaryEntry) (MultiTranslationResult, error) {
	e.contexts = append(e.contexts, translationContext)
	out := make(map[string]string, len(targetLanguages))
	for _, lang := range targetLanguages {
		out[lang] = "[" + lang + "] " + text
	}
	return MultiTranslationResult{Translations: out}, nil
}

func seedGroup(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSyncReactionFromTranslatedMessageSyncsBackToSource(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated-msg", TargetLanguage: "en",
		SourceAuthorID: "author", SourceContentSnapshot: "こんにちは",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})

	if err := service.SyncReaction(ctx, "guild", "en", "translated-msg", "👍", true); err != nil {
		t.Fatal(err)
	}

	if len(discord.reactions) != 1 {
		t.Fatalf("got %#v", discord.reactions)
	}
	if got := discord.reactions[0]; got.channelID != "ja" || got.messageID != "source-msg" || got.emoji != "👍" {
		t.Fatalf("unexpected reaction sync: %#v", got)
	}
}

func TestSyncReactionRemoveUsesOwnReaction(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated-msg", TargetLanguage: "en",
		SourceAuthorID: "author", SourceContentSnapshot: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})

	if err := service.SyncReaction(ctx, "guild", "ja", "source-msg", "👍", false); err != nil {
		t.Fatal(err)
	}
	if len(discord.removedReactions) != 1 {
		t.Fatalf("expected one own-reaction removal, got %#v", discord.removedReactions)
	}
	if got := discord.removedReactions[0]; got.channelID != "en" || got.messageID != "translated-msg" || got.emoji != "👍" {
		t.Fatalf("unexpected reaction removal: %#v", got)
	}
}

func TestReplyQuoteUsesTransferredContentWithoutRetranslationOrMention(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"en\x00translated": "> > previous pseudo reply · [Original message](https://discord.com/channels/guild/ja/older)\n\nStable translated body\nsecond line",
	}}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "orig", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorID: "source-user", SourceContentSnapshot: "こんにちは、はじめまして\n二行目",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "reply", ChannelID: "ja", GuildID: "guild", AuthorID: "reply-user",
		AuthorDisplayName: "reply-user", Content: "はじめまして！",
		ReferencedMessageID: "orig", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "> [ja] already translated quote · [引用元を見る](https://discord.com/channels/guild/en/older)\n\n[ja] translated body",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "> Stable translated body · [Original message](https://discord.com/channels/guild/en/translated)\n\n[en] はじめまして！"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("only the reply body should be translated")
	}
}

func TestForwardReusesTargetMirrorWithoutRetranslation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "original", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "元本文",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"en\x00translated": "> old quote · [Original message](https://discord.com/channels/guild/ja/old)\n\nTranslated first line\nTranslated second line",
	}}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forward", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{MessageID: "original", ChannelID: "ja", GuildID: "guild", Content: "元本文"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "-# Forwarded · https://discord.com/channels/guild/en/translated\nTranslated first line\nTranslated second line"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
	if len(translator.contexts) != 0 {
		t.Fatalf("reused forward was translated: %#v", translator.contexts)
	}
	links, err := store.MessageTargets(ctx, "ja", "forward")
	if err != nil || len(links) != 1 || links[0].SourceContentSnapshot != "元本文" {
		t.Fatalf("forward snapshot was not saved: %#v, err=%v", links, err)
	}
}

func TestForwardTranslatesUnmanagedSnapshotAndIncludesAssets(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forward", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{
			MessageID: "outside", ChannelID: "outside-channel", GuildID: "outside-guild", Content: "外部本文",
			Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/file.png?ex=1&is=2&hm=3", Filename: "file.png"}},
			Stickers:    []DiscordSticker{{ID: "sticker", FormatType: stickerFormatPNG}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "-# Forwarded · https://discord.com/channels/outside-guild/outside-channel/outside\n[en] 外部本文\nhttps://cdn.discordapp.com/file.png\nhttps://cdn.discordapp.com/stickers/sticker.png"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("external forward translation calls: %d", len(translator.contexts))
	}
}

func TestForwardWithoutTranslatableTextSkipsTranslation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forward", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{MessageID: "outside", ChannelID: "outside-channel", GuildID: "guild", Content: "https://example.com `<@123>`"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 0 {
		t.Fatalf("non-translatable forward was translated: %#v", translator.contexts)
	}
	want := "-# Forwarded · https://discord.com/channels/guild/outside-channel/outside\nhttps://example.com `<@123>`"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}

func TestForwardMirrorsIntoThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forward", ChannelID: "thread-ja", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
		AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{MessageID: "outside", ChannelID: "outside-channel", GuildID: "guild", Content: "外部本文"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || discord.sent[0].ThreadID != "thread-1" {
		t.Fatalf("unexpected thread send: %#v", discord.sent)
	}
	want := "-# Forwarded · https://discord.com/channels/guild/outside-channel/outside\n[en] 外部本文"
	if discord.sent[0].Content != want {
		t.Fatalf("got %q, want %q", discord.sent[0].Content, want)
	}
}

func TestMirroredMessageBodyStripsGeneratedHeaders(t *testing.T) {
	input := "-# Forwarded · https://discord.com/channels/g/c/m\n> quote · [Original message](https://discord.com/channels/g/c/q)\n\nbody\nsecond"
	if got, want := mirroredMessageBody(input), "body\nsecond"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHandleMessageCreatePassesGuildDescriptionAndChannelTopic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		guildNames:        map[string]string{"guild": "Ship Room"},
		guildDescriptions: map[string]string{"guild": "Release coordination server"},
		channelNames:      map[string]string{"ja": "announcements-ja"},
		channelTopics:     map[string]string{"ja": "Japanese announcements"},
	}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "出荷しました",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	if got := translator.contexts[0]; got.ServerName != "Ship Room" || got.ServerDescription != "Release coordination server" || got.ChannelName != "announcements-ja" || got.ChannelTopic != "Japanese announcements" {
		t.Fatalf("unexpected translation context: %#v", got)
	}
}

func TestHandleMessageCreatePassesGroupStyleInstructions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SetGroupStyle(ctx, "guild", "g", "gaming", ""); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "GG",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	want := ResolveStyleInstructions("gaming", "")
	if translator.contexts[0].StyleInstructions != want {
		t.Fatalf("style instructions = %q, want %q", translator.contexts[0].StyleInstructions, want)
	}
}

func TestHandleMessageCreateForwardsAttachments(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
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

func TestHandleMessageCreateForwardsAttachmentOnlyMessages(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
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

func TestHandleMessageCreateSkipsTranslationForURLOnlyContentAndReplacesAlternateURL(t *testing.T) {
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
	service.httpClient = page.Client()
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: page.URL,
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

func TestHandleMessageCreateTranslatesMarkdownLinkLabel(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "[資料](https://example.invalid)",
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
	want := "translated\nhttps://cdn.discordapp.com/attachments/1/2/image.png\nhttps://cdn.discordapp.com/attachments/1/3/archive.zip"
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

func TestMessageContentRejectsDiscordContentLimit(t *testing.T) {
	_, err := messageContentWithAssetURLs(strings.Repeat("a", discordMessageContentLimit), []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/a.png"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "Discord limit") {
		t.Fatalf("got %v", err)
	}
}

func TestHandleMessageCreatePassesRecentHistory(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en",
		SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "前の発言",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "101", ChannelID: "ja", GuildID: "guild", AuthorID: "bob",
		AuthorDisplayName: "bob", Content: "続きです",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	got := translator.contexts[0].History
	if len(got) != 1 || got[0].Author != "Alice" || got[0].Language != "ja" || got[0].Content != "前の発言" {
		t.Fatalf("unexpected history: %#v", got)
	}
}

func TestHandleMessageCreateExcludesHistoryOlderThan24Hours(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	now := time.Now().UTC()
	for _, link := range []MessageLink{
		{
			SourceMessageID: snowflakeForTime(now.Add(-25*time.Hour), 1), SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "old-target", TargetLanguage: "en",
			SourceAuthorID: "alice-id", SourceAuthorDisplayName: "Alice", SourceContentSnapshot: "昨日の発言",
		},
		{
			SourceMessageID: snowflakeForTime(now.Add(-23*time.Hour), 2), SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "recent-target", TargetLanguage: "en",
			SourceAuthorID: "bob-id", SourceAuthorDisplayName: "Bob", SourceContentSnapshot: "今日の発言",
		},
	} {
		if err := store.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: snowflakeForTime(now, 3), ChannelID: "ja", GuildID: "guild", AuthorID: "carol",
		AuthorDisplayName: "carol", Content: "続きです",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	got := translator.contexts[0].History
	if len(got) != 1 || got[0].Author != "Bob" || got[0].Content != "今日の発言" {
		t.Fatalf("unexpected history: %#v", got)
	}
}

func snowflakeForTime(t time.Time, increment uint64) string {
	return strconv.FormatUint((uint64(t.UnixMilli()-discordEpochMillis)<<22)|increment, 10)
}

func TestSyncThreadCreateAndThreadMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}
	if len(discord.threads) != 1 || discord.threads[0].channelID != "en" || discord.threads[0].name != "[en] topic" {
		t.Fatalf("unexpected thread sync: %#v", discord.threads)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "msg-in-thread", ChannelID: "thread-ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "スレッド本文",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent messages: %#v", discord.sent)
	}
	if got := discord.sent[0]; got.ThreadID != "thread-1" || got.Content != "[en] スレッド本文" {
		t.Fatalf("unexpected thread message: %#v", got)
	}

	translatorCalls := len(translator.contexts)
	err = service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "asset-in-thread", ChannelID: "thread-ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/thread.png?ex=1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != translatorCalls {
		t.Fatal("attachment-only thread message must not be translated")
	}
	if got := discord.sent[1]; got.ThreadID != "thread-1" || got.Content != "https://cdn.discordapp.com/attachments/1/2/thread.png" {
		t.Fatalf("unexpected attachment-only thread message: %#v", got)
	}

	err = service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "code-in-thread", ChannelID: "thread-ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "`fmt.Println()`",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != translatorCalls {
		t.Fatal("code-only thread message must not be translated")
	}
	if got := discord.sent[2]; got.ThreadID != "thread-1" || got.Content != "`fmt.Println()`" {
		t.Fatalf("unexpected code-only thread message: %#v", got)
	}
}

func TestHandleMessageUpdateInThreadPassesThreadIDToWebhookEdit(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveThreadLink(ctx, ThreadLink{GroupID: "g", SourceThreadID: "thread-ja", SourceChannelID: "ja", TargetThreadID: "thread-en", TargetChannelID: "en", TargetLanguage: "en"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "thread-ja", GroupID: "g",
		TargetChannelID: "thread-en", TargetMessageID: "translated-msg", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{ID: "source-msg", ChannelID: "thread-ja", GuildID: "guild", AuthorID: "u", Content: "after"}); err != nil {
		t.Fatal(err)
	}

	if len(discord.webhookEdits) != 1 {
		t.Fatalf("webhook edits: %#v", discord.webhookEdits)
	}
	if got := discord.webhookEdits[0]; got.messageID != "translated-msg" || got.threadID != "thread-en" || got.content != "[en] after" {
		t.Fatalf("unexpected webhook edit: %#v", got)
	}
}

func TestHandleMessageUpdateKeepsAlternateURLReplacement(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<link rel="alternate" hreflang="en" href="https://example.com/en">`)
	}))
	t.Cleanup(page.Close)
	service.httpClient = page.Client()
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated-msg", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "source-msg", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "see " + page.URL,
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
	service.httpClient = page.Client()
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated-msg", TargetLanguage: "en", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "source-msg", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: page.URL,
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("URL-only edit should not be translated: %#v", translator.contexts)
	}
	if len(discord.webhookEdits) != 1 || discord.webhookEdits[0].content != "https://example.com/en" {
		t.Fatalf("edits: %#v", discord.webhookEdits)
	}
	links, err := store.MessageTargets(ctx, "ja", "source-msg")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].SourceContentSnapshot != page.URL {
		t.Fatalf("snapshot not updated: %#v", links)
	}
}

func TestReplyQuoteUsesGatewayContentWithoutTranslation(t *testing.T) {
	store := newTestStore(t)
	translator := &echoTranslator{}
	service := NewService(store, &fakeDiscordAPI{}, translator)

	got, err := service.replyQuote(context.Background(), DiscordMessage{
		GuildID: "guild", ChannelID: "ja", ReferencedMessageID: "source", ReferencedMessageContent: "```go\nfmt.Println(\"hello\")\n```",
	}, "en", "en")
	if err != nil {
		t.Fatal(err)
	}
	if got != "> ```go · [Original message](https://discord.com/channels/guild/ja/source)" {
		t.Fatalf("unexpected quote: %q", got)
	}
	if len(translator.contexts) != 0 {
		t.Fatalf("reply quote should not be translated: %#v", translator.contexts)
	}
}

func TestReplyQuoteLocalizesLinkForTargetChannelLanguage(t *testing.T) {
	service := NewService(newTestStore(t), &fakeDiscordAPI{}, &echoTranslator{})
	m := DiscordMessage{
		GuildID: "guild", ChannelID: "en", ReferencedMessageID: "source",
		ReferencedMessageContent: "snippet",
	}

	tests := []struct {
		language string
		label    string
	}{
		{language: "ja", label: "引用元を見る"},
		{language: "xx-unknown", label: "Original message"},
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			got, err := service.replyQuote(context.Background(), m, "target", tt.language)
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("> snippet · [%s](https://discord.com/channels/guild/en/source)", tt.label)
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestHandleMessageDeleteInThreadPassesThreadIDToWebhookDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveThreadLink(ctx, ThreadLink{GroupID: "g", SourceThreadID: "thread-ja", SourceChannelID: "ja", TargetThreadID: "thread-en", TargetChannelID: "en", TargetLanguage: "en"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "thread-ja", GroupID: "g",
		TargetChannelID: "thread-en", TargetMessageID: "translated-msg", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageDelete(ctx, "guild", "thread-ja", "source-msg"); err != nil {
		t.Fatal(err)
	}

	if len(discord.webhookDeletes) != 1 {
		t.Fatalf("webhook deletes: %#v", discord.webhookDeletes)
	}
	if got := discord.webhookDeletes[0]; got.messageID != "translated-msg" || got.threadID != "thread-en" {
		t.Fatalf("unexpected webhook delete: %#v", got)
	}
}

func TestThreadStarterMessageIsSkippedWhenExistingMessageStartsThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "starter", ChannelID: "thread-ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "最初の本文",
		ThreadSystemMessage: true, ThreadStarterMessage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("thread starter message was translated: %#v", discord.sent)
	}
}

func TestGatewayThreadCreateDefersUntilStarterWhenParentMessageIsNotLinked(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreateFromGateway(ctx, "guild", "ja", "source-msg", "topic"); err != nil {
		t.Fatal(err)
	}
	if len(discord.threads) != 0 {
		t.Fatalf("thread should wait for source message link: %#v", discord.threads)
	}

	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated-msg", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "本文",
	}); err != nil {
		t.Fatal(err)
	}
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "starter", ChannelID: "source-msg", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
		ReferencedMessageID: "source-msg", ThreadSystemMessage: true, ThreadStarterMessage: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.messageID != "translated-msg" || got.name != "[en] topic" {
		t.Fatalf("unexpected thread sync: %#v", got)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("starter message should not be sent separately: %#v", discord.sent)
	}
}

func TestThreadMessageCreateSyncsThreadWhenMessageArrivesBeforeThreadCreate(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "first", ChannelID: "thread-ja", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
		AuthorID: "u", AuthorDisplayName: "u", Content: "最初の本文",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 || discord.threads[0].channelID != "en" || discord.threads[0].name != "[en] topic" {
		t.Fatalf("unexpected thread sync: %#v", discord.threads)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent messages: %#v", discord.sent)
	}
	if got := discord.sent[0]; got.ThreadID != "thread-1" || got.Content != "[en] 最初の本文" {
		t.Fatalf("unexpected first thread message: %#v", got)
	}
}

func TestGatewayThreadCreateAndFirstThreadMessageDoNotDuplicateThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreateFromGateway(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "first", ChannelID: "thread-ja", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
		AuthorID: "u", AuthorDisplayName: "u", Content: "最初の本文",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("duplicate target threads were created: %#v", discord.threads)
	}
	if len(discord.sent) != 1 || discord.sent[0].ThreadID != "thread-1" {
		t.Fatalf("sent messages: %#v", discord.sent)
	}
}

func TestSyncThreadCreateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}
	if err := service.SyncThreadCreate(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("duplicate target threads were created: %#v", discord.threads)
	}
}

func TestSyncThreadCreateFromMessageUsesTranslatedMessageAndTitle(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source-msg", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated-msg", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "本文",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "source-msg", "議題"); err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.messageID != "translated-msg" || got.name != "[en] 議題" {
		t.Fatalf("unexpected thread sync: %#v", got)
	}
}

func TestSyncThreadCreateInForumTargetUsesThreadOnlyChannelType(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "forum-post-ja", "議題"); err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.channelType != int(discordgo.ChannelTypeGuildForum) || got.name != "[en] 議題" || got.content != "[en] 議題" {
		t.Fatalf("unexpected forum thread sync: %#v", got)
	}
}

func TestForumInitialMessageCreatesThreadWithTranslatedInitialContentAndLink(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forum-post-ja", ChannelID: "forum-post-ja", GuildID: "guild", ParentChannelID: "ja", ThreadName: "議題",
		AuthorID: "u", AuthorDisplayName: "u", Content: "最初の本文",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.channelType != int(discordgo.ChannelTypeGuildForum) || got.name != "[en] 議題" || got.content != "[en] 最初の本文" {
		t.Fatalf("unexpected forum thread sync: %#v", got)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("forum starter should not be sent as a second message: %#v", discord.sent)
	}
	links, err := store.MessageTargets(ctx, "forum-post-ja", "forum-post-ja")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].TargetChannelID != "thread-1" || links[0].TargetMessageID != "thread-1" {
		t.Fatalf("unexpected forum starter message link: %#v", links)
	}
}

func TestForumInitialMessageSendsFirstMessageToNonForumTargetThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", ChannelType: int(discordgo.ChannelTypeGuildText), Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forum-post-ja", ChannelID: "forum-post-ja", GuildID: "guild", ParentChannelID: "ja", ThreadName: "議題",
		AuthorID: "u", AuthorDisplayName: "u", Content: "最初の本文",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 || discord.threads[0].channelID != "en" || discord.threads[0].content != "" {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if len(discord.sent) != 1 || discord.sent[0].ThreadID != "thread-1" || discord.sent[0].Content != "[en] 最初の本文" {
		t.Fatalf("sent: %#v", discord.sent)
	}
	links, err := store.MessageTargets(ctx, "forum-post-ja", "forum-post-ja")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].TargetChannelID != "thread-1" || links[0].TargetMessageID != "sent-2" {
		t.Fatalf("unexpected forum starter message link: %#v", links)
	}
}

func TestSyncThreadUpdateRenamesTargetThreads(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := service.SyncThreadCreate(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadUpdate(ctx, "guild", "thread-ja", "新タイトル"); err != nil {
		t.Fatal(err)
	}

	if len(discord.edits) != 1 {
		t.Fatalf("edits: %#v", discord.edits)
	}
	if got := discord.edits[0]; got.threadID != "thread-1" || got.name != "[en] 新タイトル" {
		t.Fatalf("unexpected thread edit: %#v", got)
	}
}

func TestSyncThreadDeleteDeletesTargetThreadsAndLinks(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := service.SyncThreadCreate(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadDelete(ctx, "thread-ja"); err != nil {
		t.Fatal(err)
	}

	if len(discord.deletes) != 1 || discord.deletes[0] != "thread-1" {
		t.Fatalf("deletes: %#v", discord.deletes)
	}
	threads, err := store.ThreadTargets(ctx, "thread-ja")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Fatalf("thread links were not deleted: %#v", threads)
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
		SourceMessageID: "source", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "existing", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "hello",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
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
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
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

	err := service.sendAndSaveLink(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}, "", WebhookSend{Content: "[en] hello", Username: "u"}, MessageLink{
		SourceMessageID: "source", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "hello",
	})
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

type selectiveFailTranslator struct {
	failLanguage string
}

func (s *selectiveFailTranslator) TranslateMulti(ctx context.Context, targetLanguages []string, text string, translationContext TranslationContext, glossary []GlossaryEntry) (MultiTranslationResult, error) {
	for _, lang := range targetLanguages {
		if lang == s.failLanguage {
			return MultiTranslationResult{}, errors.New("translation failed")
		}
	}
	out := make(map[string]string, len(targetLanguages))
	for _, lang := range targetLanguages {
		out[lang] = "[" + lang + "] " + text
	}
	return MultiTranslationResult{Translations: out}, nil
}

func seedThreeChannelGroup(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []GroupChannel{
		{GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en"},
		{GroupID: "g", GuildID: "guild", ChannelID: "fr", Language: "fr", WebhookID: "w-fr", WebhookToken: "t-fr"},
	} {
		if err := s.JoinChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleMessageCreateRateLimitBlocksTranslation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	service.SetRateLimiter(NewTokenRateLimiter(10))
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "this message should exceed the tiny rate limit",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if !strings.Contains(discord.sent[0].Content, "レート制限") {
		t.Fatalf("unexpected notification: %#v", discord.sent[0])
	}
}

func TestHandleMessageCreateFailsAllWhenTranslationFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &selectiveFailTranslator{failLanguage: "en"})
	seedThreeChannelGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "hello",
	})
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if len(discord.sent) != 1 {
		t.Fatalf("want failure notification only, got %#v", discord.sent)
	}
	if !strings.Contains(discord.sent[0].Content, "翻訳に失敗") {
		t.Fatalf("unexpected notification: %#v", discord.sent[0])
	}
	links, err := store.MessageTargets(ctx, "ja", "source")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestSyncPinPinsAndUnpinsPeers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{pinCalls: []pinCall{}}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncPin(ctx, "ja", "source", true); err != nil {
		t.Fatal(err)
	}
	if err := service.SyncPin(ctx, "ja", "source", false); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 2 {
		t.Fatalf("pin calls: %#v", discord.pinCalls)
	}
	if discord.pinCalls[0].pinned != true || discord.pinCalls[1].pinned != false {
		t.Fatalf("unexpected pin sequence: %#v", discord.pinCalls)
	}
}

func TestReplyQuoteFallsBackToGatewayReferencedMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "reply", ChannelID: "ja", GuildID: "guild", AuthorID: "reply-user",
		AuthorDisplayName: "reply-user", Content: "返信です",
		ReferencedMessageID: "orig", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "元メッセージ本文",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "> 元メッセージ本文 · [Original message](https://discord.com/channels/guild/ja/orig)\n\n[en] 返信です"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}

func TestMirrorEmptyContentReplyIncludesQuote(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "reply", ChannelID: "ja", GuildID: "guild", AuthorID: "reply-user",
		AuthorDisplayName:   "reply-user",
		ReferencedMessageID: "orig", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "引用元",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantPrefix := "> 引用元 · [Original message](https://discord.com/channels/guild/ja/orig)"
	if len(discord.sent) != 1 || discord.sent[0].Content != wantPrefix {
		t.Fatalf("got %#v, want %q", discord.sent, wantPrefix)
	}
}

func TestReplyQuoteFallsBackToStoredOriginalWhenTransferredMessageFetchFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageErrors: map[string]error{"en\x00translated": errors.New("fetch failed")}}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "orig", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "保存済み原文",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "reply", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Content: "返信", ReferencedMessageID: "orig", ReferencedMessageContent: "Gateway本文",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "> 保存済み原文 · [Original message](https://discord.com/channels/guild/en/translated)\n\n[en] 返信"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}

func TestReplyQuoteIsOmittedWhenTransferredAndOriginalContentAreUnavailable(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "orig", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "reply", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Content: "返信", ReferencedMessageID: "orig", ReferencedMessageContent: "Gateway本文",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "[en] 返信" {
		t.Fatalf("unexpected sent message: %#v", discord.sent)
	}
}

func TestFirstLineWithoutPseudoReply(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "plain", content: "first\nsecond", want: "first"},
		{name: "pseudo reply", content: "> quoted · [Original message](https://discord.com/channels/g/c/m)\n\nbody\nsecond", want: "body"},
		{name: "localized pseudo reply", content: "> > quoted · [引用元を見る](https://discord.com/channels/g/c/m)\nbody", want: "body"},
		{name: "user blockquote", content: "> user-authored quote\nbody", want: "> user-authored quote"},
		{name: "pseudo reply only", content: "> quoted · [Original message](https://discord.com/channels/g/c/m)", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLineWithoutPseudoReply(tt.content); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleMessagePinUpdateSyncsOnceAndSkipsEcho(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "source", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessagePinUpdate(ctx, "ja", "source", true); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 1 {
		t.Fatalf("pin calls: %#v", discord.pinCalls)
	}
	if err := service.HandleMessagePinUpdate(ctx, "en", "translated", true); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 1 {
		t.Fatalf("echo should be skipped, pin calls: %#v", discord.pinCalls)
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
		SourceMessageID: "source", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "same",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "same",
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
		SourceMessageID: "source", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "after",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/image.png?ex=1&hm=2"}},
	}); err != nil {
		t.Fatal(err)
	}
	links, err := store.MessageTargets(ctx, "ja", "source")
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

func TestHandleMessageCreateForwardsTTS(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "こんにちは", TTS: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || !discord.sent[0].TTS {
		t.Fatalf("expected TTS webhook send, got %#v", discord.sent)
	}
}

func TestStickerAssetURL(t *testing.T) {
	url := stickerAssetURL(DiscordSticker{ID: "1", FormatType: stickerFormatGIF})
	if url != "https://media.discordapp.net/stickers/1.gif" {
		t.Fatalf("gif: %q", url)
	}
	url = stickerAssetURL(DiscordSticker{ID: "2", FormatType: stickerFormatLottie})
	if url != "https://cdn.discordapp.com/stickers/2.png" {
		t.Fatalf("lottie: %q", url)
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

func TestForumInitialMessageForwardsTTSAndStickersToNonForumTargetThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", ChannelType: int(discordgo.ChannelTypeGuildText), Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forum-post-ja", ChannelID: "forum-post-ja", GuildID: "guild", ParentChannelID: "ja", ThreadName: "議題",
		AuthorID: "u", AuthorDisplayName: "u", Content: "最初の本文", TTS: true,
		Stickers: []DiscordSticker{{ID: "9", Name: "wave", FormatType: stickerFormatPNG}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if !discord.sent[0].TTS {
		t.Fatalf("expected TTS on deferred initial message, got %#v", discord.sent[0])
	}
	if !strings.HasSuffix(discord.sent[0].Content, "\nhttps://cdn.discordapp.com/stickers/9.png") {
		t.Fatalf("expected sticker URL on deferred initial message, got %q", discord.sent[0].Content)
	}
}

func TestForumInitialMessageSkipsTranslationForProtectedOnlyContent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	if err := store.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", ChannelType: int(discordgo.ChannelTypeGuildText), Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "forum-post-ja", ChannelID: "forum-post-ja", GuildID: "guild", ParentChannelID: "ja", ThreadName: "議題",
		AuthorID: "u", AuthorDisplayName: "u", Content: "<@123> `example` <:wave:456>",
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("only the thread name should be translated: %#v", translator.contexts)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "<@123> `example` <:wave:456>" {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

func TestHandleMessageCreateReplacesDiscordMessageLink(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "linked-ja", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "linked-en", TargetLanguage: "en",
		SourceAuthorID: "author", SourceContentSnapshot: "referenced",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u",
		Content:           "see " + MessageJumpURL("guild", "ja", "linked-ja"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "[en] see " + MessageJumpURL("guild", "en", "linked-en")
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}
