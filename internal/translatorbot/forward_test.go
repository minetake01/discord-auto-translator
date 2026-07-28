package translatorbot

// SPEC 3.4

import (
	"context"
	"testing"
)

// SPEC 3.4
func TestForwardReusesTargetMirrorWithoutRetranslation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000003", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "元本文",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{messageContents: map[string]string{
		"en\x00translated": "> old quote · [Source](https://discord.com/channels/guild/ja/old)\n\nTranslated first line\nTranslated second line",
	}}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000004", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{MessageID: "100000000000000003", ChannelID: "ja", GuildID: "guild", Content: "元本文"},
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
	links, err := store.MessageTargets(ctx, "ja", "100000000000000004")
	if err != nil || len(links) != 1 || links[0].SourceContentSnapshot != "元本文" {
		t.Fatalf("forward snapshot was not saved: %#v, err=%v", links, err)
	}
}

// SPEC 3.4
func TestForwardTranslatesUnmanagedSnapshotAndIncludesAssets(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000004", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{
			MessageID: "100000000000000016", ChannelID: "outside-channel", GuildID: "outside-guild", Content: "外部本文",
			Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/file.png?ex=1&is=2&hm=3", Filename: "file.png"}},
			Stickers:    []DiscordSticker{{ID: "sticker", FormatType: stickerFormatPNG}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "-# Forwarded · https://discord.com/channels/outside-guild/outside-channel/100000000000000016\n[en] 外部本文\nhttps://cdn.discordapp.com/file.png\nhttps://cdn.discordapp.com/stickers/sticker.png"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
	if len(translator.contexts) != 1 {
		t.Fatalf("external forward translation calls: %d", len(translator.contexts))
	}
}

// SPEC 3.4
func TestForwardWithoutTranslatableTextSkipsTranslation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000004", ChannelID: "ja", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{MessageID: "100000000000000016", ChannelID: "outside-channel", GuildID: "guild", Content: "https://example.com `<@123>`"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != 0 {
		t.Fatalf("non-translatable forward was translated: %#v", translator.contexts)
	}
	want := "-# Forwarded · https://discord.com/channels/guild/outside-channel/100000000000000016\nhttps://example.com `<@123>`"
	if len(discord.sent) != 1 || discord.sent[0].Content != want {
		t.Fatalf("got %#v, want %q", discord.sent, want)
	}
}

// SPEC 3.4
func TestForwardMirrorsIntoThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000004", ChannelID: "100000000000000005", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
		AuthorID: "u", AuthorDisplayName: "u",
		ForwardedMessage: &DiscordForwardedMessage{MessageID: "100000000000000016", ChannelID: "outside-channel", GuildID: "guild", Content: "外部本文"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 1 || discord.sent[0].ThreadID != "thread-1" {
		t.Fatalf("unexpected thread send: %#v", discord.sent)
	}
	want := "-# Forwarded · https://discord.com/channels/guild/outside-channel/100000000000000016\n[en] 外部本文"
	if discord.sent[0].Content != want {
		t.Fatalf("got %q, want %q", discord.sent[0].Content, want)
	}
}
