package translatorbot

import (
	"context"
	"testing"
)

func TestSyncReactionFromTranslatedMessageSyncsBackToSource(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000015", TargetLanguage: "en",
		SourceAuthorID: "author", SourceContentSnapshot: "こんにちは",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})

	if err := service.SyncReaction(ctx, "guild", "en", "100000000000000015", "👍", true); err != nil {
		t.Fatal(err)
	}

	if len(discord.reactions) != 1 {
		t.Fatalf("got %#v", discord.reactions)
	}
	if got := discord.reactions[0]; got.channelID != "ja" || got.messageID != "100000000000000006" || got.emoji != "👍" {
		t.Fatalf("unexpected reaction sync: %#v", got)
	}
}

func TestSyncReactionRemoveUsesOwnReaction(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000015", TargetLanguage: "en",
		SourceAuthorID: "author", SourceContentSnapshot: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})

	if err := service.SyncReaction(ctx, "guild", "ja", "100000000000000006", "👍", false); err != nil {
		t.Fatal(err)
	}
	if len(discord.removedReactions) != 1 {
		t.Fatalf("expected one own-reaction removal, got %#v", discord.removedReactions)
	}
	if got := discord.removedReactions[0]; got.channelID != "en" || got.messageID != "100000000000000015" || got.emoji != "👍" {
		t.Fatalf("unexpected reaction removal: %#v", got)
	}
}

func TestSyncPinPinsAndUnpinsPeers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{pinCalls: []pinCall{}}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncPin(ctx, "ja", "100000000000000001", true); err != nil {
		t.Fatal(err)
	}
	if err := service.SyncPin(ctx, "ja", "100000000000000001", false); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 2 {
		t.Fatalf("pin calls: %#v", discord.pinCalls)
	}
	if discord.pinCalls[0].pinned != true || discord.pinCalls[1].pinned != false {
		t.Fatalf("unexpected pin sequence: %#v", discord.pinCalls)
	}
}

func TestHandleMessagePinUpdateSyncsOnceAndSkipsEcho(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000018", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessagePinUpdate(ctx, "ja", "100000000000000001", true); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 1 {
		t.Fatalf("pin calls: %#v", discord.pinCalls)
	}
	if err := service.HandleMessagePinUpdate(ctx, "en", "100000000000000018", true); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 1 {
		t.Fatalf("echo should be skipped, pin calls: %#v", discord.pinCalls)
	}
}

func TestHandleMessagePinUpdateInitialFalseOnlySeedsState(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000018", TargetLanguage: "en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessagePinUpdate(ctx, "ja", "100000000000000001", false); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 0 {
		t.Fatalf("initial false should not call pin APIs: %#v", discord.pinCalls)
	}
	pinned, known, err := store.GetPinState(ctx, "ja", "100000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if !known || pinned {
		t.Fatalf("expected source false pin state, got known=%v pinned=%v", known, pinned)
	}
	pinned, known, err = store.GetPinState(ctx, "en", "100000000000000018")
	if err != nil {
		t.Fatal(err)
	}
	if !known || pinned {
		t.Fatalf("expected peer false pin state, got known=%v pinned=%v", known, pinned)
	}

	if err := service.HandleMessagePinUpdate(ctx, "en", "100000000000000018", false); err != nil {
		t.Fatal(err)
	}
	if len(discord.pinCalls) != 0 {
		t.Fatalf("seeded false echo should not call pin APIs: %#v", discord.pinCalls)
	}
}
