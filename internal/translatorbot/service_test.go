package translatorbot

import (
	"context"
	"fmt"
	"testing"
)

type fakeDiscordAPI struct {
	sent      []WebhookSend
	reactions []reactionCall
	threads   []threadCall
	nextID    int
}

type reactionCall struct {
	channelID string
	messageID string
	emoji     string
}

type threadCall struct {
	channelID string
	name      string
}

func (f *fakeDiscordAPI) CreateWebhook(channelID, name string) (id, token string, err error) {
	return "webhook-" + channelID, "token-" + channelID, nil
}

func (f *fakeDiscordAPI) SendWebhook(webhookID, token string, msg WebhookSend) (messageID string, err error) {
	f.nextID++
	f.sent = append(f.sent, msg)
	return fmt.Sprintf("sent-%d", f.nextID), nil
}

func (f *fakeDiscordAPI) EditWebhook(webhookID, token, messageID, content string) error { return nil }
func (f *fakeDiscordAPI) DeleteWebhook(webhookID, token, messageID string) error        { return nil }

func (f *fakeDiscordAPI) AddReaction(channelID, messageID, emoji string) error {
	f.reactions = append(f.reactions, reactionCall{channelID: channelID, messageID: messageID, emoji: emoji})
	return nil
}

func (f *fakeDiscordAPI) RemoveReaction(channelID, messageID, emoji, userID string) error {
	return nil
}

func (f *fakeDiscordAPI) PinMessage(channelID, messageID string) error   { return nil }
func (f *fakeDiscordAPI) UnpinMessage(channelID, messageID string) error { return nil }

func (f *fakeDiscordAPI) CreateThread(channelID, name string) (threadID string, err error) {
	f.nextID++
	f.threads = append(f.threads, threadCall{channelID: channelID, name: name})
	return fmt.Sprintf("thread-%d", f.nextID), nil
}

type echoTranslator struct{}

func (echoTranslator) Translate(ctx context.Context, targetLanguage, text string, history []ChatContextMessage) (string, error) {
	return "[" + targetLanguage + "] " + text, nil
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
	service := NewService(store, discord, echoTranslator{})

	if err := service.SyncReaction(ctx, "guild", "en", "translated-msg", "👍", "bot", true); err != nil {
		t.Fatal(err)
	}

	if len(discord.reactions) != 1 {
		t.Fatalf("got %#v", discord.reactions)
	}
	if got := discord.reactions[0]; got.channelID != "ja" || got.messageID != "source-msg" || got.emoji != "👍" {
		t.Fatalf("unexpected reaction sync: %#v", got)
	}
}

func TestReplyQuoteIncludesMentionAndTruncatedFirstLine(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, echoTranslator{})
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

	want := "<@source-user>\n> [en] こんにちは\n-# [original message](https://discord.com/channels/guild/en/translated)\n[en] はじめまして！"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}

func TestSyncThreadCreateAndThreadMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "thread-ja", "topic"); err != nil {
		t.Fatal(err)
	}
	if len(discord.threads) != 1 || discord.threads[0].channelID != "en" || discord.threads[0].name != "topic" {
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
