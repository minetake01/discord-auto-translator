package translatorbot

// SPEC 3.3

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// SPEC 3.3
func TestReplyQuoteUsesTransferredContentWithoutRetranslationOrMention(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"en\x00translated": "> > previous pseudo reply · [Source](https://discord.com/channels/guild/ja/older)\n\nStable translated body\nsecond line",
	}}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorID: "source-user", SourceContentSnapshot: "こんにちは、はじめまして\n二行目",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "reply-user",
		AuthorDisplayName: "reply-user", Content: "はじめまして！",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "> [ja] already translated quote · [引用元を見る](https://discord.com/channels/guild/en/older)\n\n[ja] translated body",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "> -# Stable translated body · [Source](https://discord.com/channels/guild/en/translated)\n\n[en] はじめまして！"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("only the reply body should be translated")
	}
	replies, err := store.MessageTargetsReplyingTo(ctx, "ja", "100000000000000002")
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].SourceMessageID != "100000000000000008" || replies[0].TargetMessageID != "sent-1" {
		t.Fatalf("reply reference was not persisted: %#v", replies)
	}
}

// SPEC 3.3
func TestReplyQuoteUsesGatewayContentWithoutTranslation(t *testing.T) {
	store := newTestStore(t)
	translator := &echoTranslator{}
	service := NewService(store, &fakeDiscordAPI{}, translator)

	got, err := service.replyQuote(context.Background(), DiscordMessage{
		GuildID: "guild", ChannelID: "ja", ReferencedMessageID: "100000000000000001", ReferencedMessageContent: "```go\nfmt.Println(\"hello\")\n```",
	}, "en", "en")
	if err != nil {
		t.Fatal(err)
	}
	if got != "> -# ```go · [Source](https://discord.com/channels/guild/ja/100000000000000001)" {
		t.Fatalf("unexpected quote: %q", got)
	}
	if len(translator.contexts) != 0 {
		t.Fatalf("reply quote should not be translated: %#v", translator.contexts)
	}
}

// SPEC 3.3
func TestReplyQuoteLocalizesLinkForTargetChannelLanguage(t *testing.T) {
	service := NewService(newTestStore(t), &fakeDiscordAPI{}, &echoTranslator{})
	m := DiscordMessage{
		GuildID: "guild", ChannelID: "en", ReferencedMessageID: "100000000000000001",
		ReferencedMessageContent: "snippet",
	}

	tests := []struct {
		language string
		label    string
	}{
		{language: "ja", label: "引用元を見る"},
		{language: "xx-unknown", label: "Source"},
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			got, err := service.replyQuote(context.Background(), m, "target", tt.language)
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("> -# snippet · [%s](https://discord.com/channels/guild/en/100000000000000001)", tt.label)
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

// SPEC 3.3
func TestNormalizeMarkdownHeaderSnippet(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "h1", line: "# Title", want: "-# Title"},
		{name: "h2", line: "## Title", want: "-# Title"},
		{name: "h3 with trailing hashes", line: "### Title ###", want: "-# Title"},
		{name: "plain text", line: "plain text", want: "-# plain text"},
		{name: "no space after hash", line: "#no-space", want: "-# #no-space"},
		{name: "forwarded header", line: "-# Forwarded · https://discord.com/channels/g/c/m", want: "-# Forwarded · https://discord.com/channels/g/c/m"},
		{name: "empty title", line: "# ", want: "-# #"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMarkdownHeaderSnippet(tt.line); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// SPEC 3.3
func TestReplyQuoteConvertsMarkdownHeaderSnippet(t *testing.T) {
	service := NewService(newTestStore(t), &fakeDiscordAPI{}, &echoTranslator{})
	got, err := service.replyQuote(context.Background(), DiscordMessage{
		GuildID: "guild", ChannelID: "en", ReferencedMessageID: "100000000000000001",
		ReferencedMessageContent: "## Important\nbody",
	}, "target", "en")
	if err != nil {
		t.Fatal(err)
	}
	want := "> -# Important · [Source](https://discord.com/channels/guild/en/100000000000000001)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// SPEC 3.3
func TestHandleMessageDeleteReplacesExistingReplyQuote(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"ja" + "\x00" + "100000000000000015": "> -# 古いスニペット · [引用元を見る](https://discord.com/channels/guild/en/100000000000000014)\n\n[ja] 返信本文",
	}}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000014", TargetLanguage: "en",
		SourceAuthorID: "alice", SourceContentSnapshot: "original",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLinkWithReference(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "en", GroupID: "g",
		TargetChannelID: "ja", TargetMessageID: "100000000000000015", TargetLanguage: "ja",
		SourceAuthorID: "bob", SourceContentSnapshot: "reply body",
	}, MessageReference{MessageID: "100000000000000014", ChannelID: "en"}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageDelete(ctx, "guild", "ja", "100000000000000001"); err != nil {
		t.Fatal(err)
	}

	if len(discord.webhookEdits) != 1 {
		t.Fatalf("webhook edits: %#v", discord.webhookEdits)
	}
	if got := discord.webhookEdits[0]; got.messageID != "100000000000000015" || got.threadID != "" || got.content != "> -# 元のメッセージが削除されました\n\n[ja] 返信本文" {
		t.Fatalf("unexpected webhook edit: %#v", got)
	}
	if len(discord.webhookDeletes) != 1 || discord.webhookDeletes[0].messageID != "100000000000000014" {
		t.Fatalf("webhook deletes: %#v", discord.webhookDeletes)
	}
	replies, err := store.MessageTargetsReplyingTo(ctx, "en", "100000000000000014")
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 0 {
		t.Fatalf("deleted reference remains: %#v", replies)
	}
}

// SPEC 3.3
func TestReplyQuoteFallsBackToGatewayReferencedMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "reply-user",
		AuthorDisplayName: "reply-user", Content: "返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "元メッセージ本文",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "> -# 元メッセージ本文 · [Source](https://discord.com/channels/guild/ja/100000000000000002)\n\n[en] 返信です"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}

// SPEC 3.3
func TestMirrorEmptyContentReplyIncludesQuote(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "reply-user",
		AuthorDisplayName:   "reply-user",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "引用元",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantPrefix := "> -# 引用元 · [Source](https://discord.com/channels/guild/ja/100000000000000002)"
	if len(discord.sent) != 1 || discord.sent[0].Content != wantPrefix {
		t.Fatalf("got %#v, want %q", discord.sent, wantPrefix)
	}
}

// SPEC 3.3
func TestReplyQuoteFallsBackToStoredOriginalWhenTransferredMessageFetchFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageErrors: map[string]error{"en\x00translated": errors.New("fetch failed")}}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "保存済み原文",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Content: "返信", ReferencedMessageID: "100000000000000002", ReferencedMessageContent: "Gateway本文",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "> -# 保存済み原文 · [Source](https://discord.com/channels/guild/en/translated)\n\n[en] 返信"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}

// SPEC 3.3
func TestReplyQuoteIsOmittedWhenTransferredAndOriginalContentAreUnavailable(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Content: "返信", ReferencedMessageID: "100000000000000002", ReferencedMessageContent: "Gateway本文",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || discord.sent[0].Content != "[en] 返信" {
		t.Fatalf("unexpected sent message: %#v", discord.sent)
	}
}

// SPEC 3.3
func TestFirstLineWithoutPseudoReply(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "plain", content: "first\nsecond", want: "first"},
		{name: "pseudo reply", content: "> quoted · [Source](https://discord.com/channels/g/c/m)\n\nbody\nsecond", want: "body"},
		{name: "localized pseudo reply", content: "> > quoted · [引用元を見る](https://discord.com/channels/g/c/m)\nbody", want: "body"},
		{name: "user blockquote", content: "> user-authored quote\nbody", want: "> user-authored quote"},
		{name: "pseudo reply only", content: "> quoted · [Source](https://discord.com/channels/g/c/m)", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLineWithoutPseudoReply(tt.content); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
