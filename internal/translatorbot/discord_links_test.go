package translatorbot

import (
	"context"
	"testing"
)

func seedJAENGroup(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
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
}

func TestReplaceDiscordRefsChannelURL(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)

	got := ReplaceDiscordRefs(ctx, store, "guild", "see https://discord.com/channels/guild/ja", "en")
	want := "see https://discord.com/channels/guild/en"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceDiscordRefsMessageURL(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "msg-ja", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "msg-en", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}

	got := ReplaceDiscordRefs(ctx, store, "guild", "jump https://discord.com/channels/guild/ja/msg-ja", "en")
	want := "jump " + MessageJumpURL("guild", "en", "msg-en")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceDiscordRefsChannelMention(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)

	got := ReplaceDiscordRefs(ctx, store, "guild", "go to <#ja> please", "en")
	want := "go to <#en> please"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceDiscordRefsThreadURL(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)
	if err := store.SaveThreadLink(ctx, ThreadLink{
		GroupID: "g", SourceThreadID: "thread-ja", SourceChannelID: "ja",
		TargetThreadID: "thread-en", TargetChannelID: "en", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}

	got := ReplaceDiscordRefs(ctx, store, "guild", "https://discord.com/channels/guild/thread-ja", "en")
	want := "https://discord.com/channels/guild/thread-en"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReplaceDiscordRefsUnmanagedChannelUnchanged(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)

	input := "https://discord.com/channels/guild/other"
	got := ReplaceDiscordRefs(ctx, store, "guild", input, "en")
	if got != input {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestReplaceDiscordRefsMessageWithoutLinkUnchanged(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)

	input := "https://discord.com/channels/guild/ja/unmirrored"
	got := ReplaceDiscordRefs(ctx, store, "guild", input, "en")
	if got != input {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestReplaceDiscordRefsOtherGuildUnchanged(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)

	input := "https://discord.com/channels/other-guild/ja"
	got := ReplaceDiscordRefs(ctx, store, "guild", input, "en")
	if got != input {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestReplaceDiscordRefsSameLanguageUnchanged(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedJAENGroup(t, store)

	input := "https://discord.com/channels/guild/ja"
	got := ReplaceDiscordRefs(ctx, store, "guild", input, "ja")
	if got != input {
		t.Fatalf("got %q, want %q", got, input)
	}
}
