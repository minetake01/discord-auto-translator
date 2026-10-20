package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestReplyQuoteUsesTransferredContentWithoutRetranslationOrMention(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"en\x00700000000000000019": "> > previous pseudo reply · [Source](https://discord.com/channels/guild/ja/older)\n\nStable translated body\nsecond line",
	}}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000019", TargetLanguage: "en",
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

	want := "> -# Stable translated body · [Source](https://discord.com/channels/guild/en/700000000000000019)\n\n[en] はじめまして！"
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
	if len(replies) != 1 || replies[0].SourceMessageID != "100000000000000008" || replies[0].TargetMessageID != "900000000000000001" {
		t.Fatalf("reply reference was not persisted: %#v", replies)
	}
}

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

func TestReplyQuoteSuppressesSnippetLinkEmbeds(t *testing.T) {
	service := NewService(newTestStore(t), &fakeDiscordAPI{}, &echoTranslator{})
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "bare URL", content: "see https://example.com/a", want: "-# see <https://example.com/a>"},
		{name: "already suppressed", content: "see <https://example.com/a>", want: "-# see <https://example.com/a>"},
		{name: "masked link", content: "[doc](https://example.com/a)", want: "-# [doc](https://example.com/a)"},
		{name: "truncated URL", content: "see https://example.com/" + strings.Repeat("p", 40), want: "-# see <https://example.com/ppppppppppppp...>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.replyQuote(context.Background(), DiscordMessage{
				GuildID: "guild", ChannelID: "en", ReferencedMessageID: "100000000000000001",
				ReferencedMessageContent: tt.content,
			}, "target", "en")
			if err != nil {
				t.Fatal(err)
			}
			want := "> " + tt.want + " · [Source](https://discord.com/channels/guild/en/100000000000000001)"
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

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

func TestHandleMessageUpdateKeepsReplyQuote(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "元メッセージ本文",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || !strings.HasPrefix(discord.sent[0].Content, "> -# 元メッセージ本文") {
		t.Fatalf("sent: %#v", discord.sent)
	}

	// A gateway edit carries no reply reference, so the edit must restore it.
	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "返信を直しました",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 1 {
		t.Fatalf("edits: %#v", discord.webhookEdits)
	}
	want := "> -# 元メッセージ本文 · [Source](https://discord.com/channels/guild/ja/100000000000000002)\n\n[en] 返信を直しました"
	if discord.webhookEdits[0].content != want {
		t.Fatalf("edit content = %q, want %q", discord.webhookEdits[0].content, want)
	}
}

func TestHandleMessageUpdateKeepsTransferredReplyQuote(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"en\x00700000000000000019": "> -# 古い引用 · [Source](https://discord.com/channels/guild/ja/old)\n\nStable translated body",
	}}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000019", TargetLanguage: "en",
		SourceContentSnapshot: "元のメッセージ本文",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "元メッセージ本文",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || !strings.HasPrefix(discord.sent[0].Content, "> -# Stable translated body") {
		t.Fatalf("sent: %#v", discord.sent)
	}

	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "返信を直しました",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 1 {
		t.Fatalf("edits: %#v", discord.webhookEdits)
	}
	want := "> -# Stable translated body · [Source](https://discord.com/channels/guild/en/700000000000000019)\n\n[en] 返信を直しました"
	if discord.webhookEdits[0].content != want {
		t.Fatalf("edit content = %q, want %q", discord.webhookEdits[0].content, want)
	}
}

func TestHandleMessageUpdateKeepsDeletedOriginalNotice(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"en\x00700000000000000019": "Stable translated body",
	}}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000019", TargetLanguage: "en",
		SourceContentSnapshot: "元のメッセージ本文",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "元のメッセージ本文",
	}); err != nil {
		t.Fatal(err)
	}

	// The referenced message is deleted: the reply mirror's quote is replaced
	// by the deletion notice.
	if err := service.HandleMessageDelete(ctx, "guild", "ja", "100000000000000002"); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 1 || discord.webhookEdits[0].content != "> -# The original message was deleted\n\n[en] 返信です" {
		t.Fatalf("deletion edits: %#v", discord.webhookEdits)
	}

	// Editing the reply afterwards must not drop that notice.
	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "返信を直しました",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 2 {
		t.Fatalf("edits: %#v", discord.webhookEdits)
	}
	if got := discord.webhookEdits[1].content; got != "> -# The original message was deleted\n\n[en] 返信を直しました" {
		t.Fatalf("edit content = %q", got)
	}
}

func TestHandleMessageUpdateRefreshesReplyingQuotes(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "元のメッセージ本文",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "元のメッセージ本文",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 2 || !strings.HasPrefix(discord.sent[1].Content, "> -# [en] 元のメッセージ本文") {
		t.Fatalf("sent: %#v", discord.sent)
	}

	// Editing the referenced message rewrites its mirror and then the quote
	// of the reply's mirror.
	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "変更後の本文",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 2 {
		t.Fatalf("edits: %#v", discord.webhookEdits)
	}
	if got := discord.webhookEdits[0]; got.messageID != "900000000000000001" || got.content != "[en] 変更後の本文" {
		t.Fatalf("referenced edit = %#v", got)
	}
	want := "> -# [en] 変更後の本文 · [Source](https://discord.com/channels/guild/en/900000000000000001)\n\n[en] 返信です"
	if got := discord.webhookEdits[1]; got.messageID != "900000000000000002" || got.content != want {
		t.Fatalf("reply quote edit = %#v, want %q", got, want)
	}
}

func TestHandleMessageUpdateSkipsReplyingQuotesWhenSnippetUnchanged(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "一行目\n二行目",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: "一行目\n二行目",
	}); err != nil {
		t.Fatal(err)
	}

	// An edit that does not change the quoted first line refreshes nothing
	// beyond the referenced message's own mirror.
	if err := service.HandleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "一行目\n二行目改",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 1 || discord.webhookEdits[0].messageID != "900000000000000001" {
		t.Fatalf("edits: %#v", discord.webhookEdits)
	}
}

func TestFetchedContentDoesNotAccumulateEmbedLine(t *testing.T) {
	embeds := []*discordgo.MessageEmbed{{Title: "ページのタイトル"}}
	first := contentWithEmbedQuoteText("body\nhttps://example.com/page", embeds)
	second := contentWithEmbedQuoteText(first, embeds)
	if first != "body\nhttps://example.com/page\nページのタイトル" {
		t.Fatalf("first fetch = %q", first)
	}
	if second != first {
		t.Fatalf("refetch accumulated the embed line: %q", second)
	}
}

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

func TestReplyQuoteFallsBackToStoredOriginalWhenTransferredMessageFetchFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{messageErrors: map[string]error{"en\x00700000000000000019": errors.New("fetch failed")}}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000019", TargetLanguage: "en",
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
	want := "> -# 保存済み原文 · [Source](https://discord.com/channels/guild/en/700000000000000019)\n\n[en] 返信"
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
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000019", TargetLanguage: "en",
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

func TestReplyQuoteAttachmentOnlyReferencedMessage(t *testing.T) {
	service := NewService(newTestStore(t), &fakeDiscordAPI{}, &echoTranslator{})
	got, err := service.replyQuote(context.Background(), DiscordMessage{
		GuildID: "guild", ChannelID: "en",
		ReferencedMessageID: "100000000000000001", ReferencedMessageChannelID: "en",
		ReferencedMessageAttachments: []DiscordAttachment{
			{URL: "https://cdn.discordapp.com/attachments/1/2/image.png", Filename: "image.png", ContentType: "image/png"},
			{URL: "https://cdn.discordapp.com/attachments/1/3/photo.jpg", Filename: "photo.jpg", ContentType: "image/jpeg"},
		},
	}, "target", "en")
	if err != nil {
		t.Fatal(err)
	}
	want := "> -# 2 attachment(s) · [Source](https://discord.com/channels/guild/en/100000000000000001)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplyQuoteAttachmentOnlyViaStoredLink(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "700000000000000019", TargetLanguage: "en",
		SourceContentSnapshot: "",
		SourceImageAttachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/image.png", Filename: "image.png", ContentType: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := service.replyQuote(ctx, DiscordMessage{
		GuildID: "guild", ChannelID: "ja", ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
	}, "en", "en")
	if err != nil {
		t.Fatal(err)
	}
	want := "> -# 1 attachment(s) · [Source](https://discord.com/channels/guild/en/700000000000000019)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHandleMessageCreateAttachmentOnlyReplyQuote(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000008", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "u", Content: "画像への返信です",
		ReferencedMessageID: "100000000000000002", ReferencedMessageChannelID: "ja",
		ReferencedMessageAttachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/image.png", Filename: "image.png", ContentType: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}

	wantPrefix := "> -# 1 attachment(s) · [Source](https://discord.com/channels/guild/ja/100000000000000002)"
	if len(discord.sent) != 1 || discord.sent[0].Content != wantPrefix+"\n\n[en] 画像への返信です" {
		t.Fatalf("sent: %#v", discord.sent)
	}
}

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
