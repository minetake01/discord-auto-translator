package translatorbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	files       []sentFile
}

type sentFile struct {
	name        string
	contentType string
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

func (f *fakeDiscordAPI) CreateWebhook(channelID, name string) (id, token string, err error) {
	return "webhook-" + channelID, "token-" + channelID, nil
}

func (f *fakeDiscordAPI) SendChannelMessage(channelID, content string) error {
	f.nextID++
	f.sent = append(f.sent, WebhookSend{Content: content})
	return nil
}

func (f *fakeDiscordAPI) SendWebhook(webhookID, token string, msg WebhookSend) (messageID string, err error) {
	var files []WebhookFile
	for _, file := range msg.Files {
		content, err := io.ReadAll(file.Reader)
		if err != nil {
			return "", err
		}
		files = append(files, WebhookFile{Name: file.Name, ContentType: file.ContentType, Reader: strings.NewReader(string(content))})
	}
	msg.Files = files
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

func (f *fakeDiscordAPI) CreateThread(channelID string, channelType int, name, initialMessage string, files []WebhookFile) (threadID, initialMessageID string, err error) {
	f.nextID++
	threadID = fmt.Sprintf("thread-%d", f.nextID)
	if isThreadOnlyChannelType(channelType) {
		initialMessageID = threadID
	}
	f.threads = append(f.threads, threadCall{channelID: channelID, channelType: channelType, name: name, content: initialMessage, files: readSentFiles(files)})
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

func readSentFiles(files []WebhookFile) []sentFile {
	out := make([]sentFile, 0, len(files))
	for _, file := range files {
		content, _ := io.ReadAll(file.Reader)
		out = append(out, sentFile{name: file.Name, contentType: file.ContentType, content: string(content)})
	}
	return out
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

func TestReplyQuoteIncludesMentionAndTruncatedFirstLine(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
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
		ReferencedMessageID: "orig", MentionAuthor: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "<@source-user>\n> [en] こんにちは、はじめまして\n-# [original message](https://discord.com/channels/guild/en/translated)\n[en] はじめまして！"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
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

func TestHandleMessageCreateForwardsAttachments(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		fmt.Fprint(w, "png-bytes")
	}))
	t.Cleanup(fileServer.Close)
	service.httpClient = fileServer.Client()

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "画像です",
		Attachments: []DiscordAttachment{{URL: fileServer.URL + "/image.png", Filename: "image.png", ContentType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.sent) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
	}
	if got := discord.sent[0]; len(got.Files) != 1 || got.Files[0].Name != "image.png" || got.Files[0].ContentType != "image/png" {
		t.Fatalf("unexpected files: %#v", got.Files)
	}
	content, err := io.ReadAll(discord.sent[0].Files[0].Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "png-bytes" {
		t.Fatalf("file content = %q", content)
	}
}

func TestHandleMessageCreateForwardsAttachmentOnlyMessages(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "file-only")
	}))
	t.Cleanup(fileServer.Close)
	service.httpClient = fileServer.Client()

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "source", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Attachments: []DiscordAttachment{{URL: fileServer.URL + "/photo.jpg", Filename: "photo.jpg", ContentType: "image/jpeg"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 0 {
		t.Fatalf("blank content should not be translated: %#v", translator.contexts)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "" || len(discord.sent[0].Files) != 1 {
		t.Fatalf("sent: %#v", discord.sent)
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
		SourceAuthorID: "alice", SourceContentSnapshot: "前の発言",
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
	if len(got) != 1 || got[0].Author != "alice" || got[0].Language != "ja" || got[0].Content != "前の発言" {
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
			SourceAuthorID: "alice", SourceContentSnapshot: "昨日の発言",
		},
		{
			SourceMessageID: snowflakeForTime(now.Add(-23*time.Hour), 2), SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "recent-target", TargetLanguage: "en",
			SourceAuthorID: "bob", SourceContentSnapshot: "今日の発言",
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
	if len(got) != 1 || got[0].Author != "bob" || got[0].Content != "今日の発言" {
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
	service := NewService(store, discord, &echoTranslator{})
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
		ReferencedMessageAuthorID: "source-user", ReferencedMessageContent: "元メッセージ本文",
		MentionAuthor: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "<@source-user>\n> [en] 元メッセージ本文\n-# [original message](https://discord.com/channels/guild/ja/orig)\n[en] 返信です"
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

	wantPrefix := "> [en] 引用元\n-# [original message](https://discord.com/channels/guild/ja/orig)"
	if len(discord.sent) != 1 || discord.sent[0].Content != wantPrefix {
		t.Fatalf("got %#v, want %q", discord.sent, wantPrefix)
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
	url, contentType, skip := stickerAssetURL(DiscordSticker{ID: "1", FormatType: stickerFormatGIF})
	if url != "https://media.discordapp.net/stickers/1.gif" || contentType != "image/gif" || skip {
		t.Fatalf("gif: %q %q %v", url, contentType, skip)
	}
	url, contentType, skip = stickerAssetURL(DiscordSticker{ID: "2", FormatType: stickerFormatLottie})
	if url != "https://cdn.discordapp.com/stickers/2.png" || contentType != "image/png" || !skip {
		t.Fatalf("lottie: %q %q %v", url, contentType, skip)
	}
}

func TestStickerFilesDownloadsSticker(t *testing.T) {
	ctx := context.Background()
	service := NewService(newTestStore(t), &fakeDiscordAPI{}, &echoTranslator{})
	service.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusOK)
		_, _ = rec.WriteString("sticker-bytes")
		return rec.Result(), nil
	})}

	files, err := service.stickerFiles(ctx, []DiscordSticker{{ID: "9", Name: "wave", FormatType: stickerFormatPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "wave.png" {
		t.Fatalf("unexpected files: %#v", files)
	}
}

func TestStickerFilesSkipsLottieAndLogs(t *testing.T) {
	ctx := context.Background()
	service := NewService(newTestStore(t), &fakeDiscordAPI{}, &echoTranslator{})
	service.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusNotFound)
		return rec.Result(), nil
	})}

	var buf bytes.Buffer
	original := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(original) })

	files, err := service.stickerFiles(ctx, []DiscordSticker{{ID: "lottie-1", Name: "wave", FormatType: stickerFormatLottie}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected lottie sticker to be skipped, got %#v", files)
	}
	if !strings.Contains(buf.String(), "skip lottie sticker lottie-1 (wave)") {
		t.Fatalf("expected skip log, got %q", buf.String())
	}
}

func TestForumInitialMessageForwardsTTSAndStickersToNonForumTargetThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	service.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusOK)
		_, _ = rec.WriteString("sticker-bytes")
		return rec.Result(), nil
	})}
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
	if len(discord.sent[0].Files) != 1 || discord.sent[0].Files[0].Name != "wave.png" {
		t.Fatalf("expected sticker file on deferred initial message, got %#v", discord.sent[0].Files)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
