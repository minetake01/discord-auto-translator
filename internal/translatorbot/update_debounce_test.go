package translatorbot

import (
	"context"
	"testing"
	"time"
)

func waitForWebhookEdits(t *testing.T, discord *fakeDiscordAPI, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		discord.mu.Lock()
		got := len(discord.webhookEdits)
		discord.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d webhook edits, got %d", n, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBotEditBurstIsTranslatedOnceWithLatestContent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	if err := store.AddAllowedSource(ctx, "guild", SourceTypeBot, "123456789012345678", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "frame 0",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	service.editDebounce = newEditDebouncer(200 * time.Millisecond)

	for _, content := range []string{"frame 1", "frame 2", "final"} {
		if err := service.ScheduleMessageUpdate(ctx, DiscordMessage{
			ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "123456789012345678", Bot: true, Content: content,
		}); err != nil {
			t.Fatal(err)
		}
	}
	waitForWebhookEdits(t, discord, 1)
	time.Sleep(300 * time.Millisecond)

	translator.mu.Lock()
	translations := len(translator.contexts)
	translator.mu.Unlock()
	discord.mu.Lock()
	edits := append([]webhookEditCall(nil), discord.webhookEdits...)
	discord.mu.Unlock()
	if translations != 1 || len(edits) != 1 || edits[0].content != "[en] final" {
		t.Fatalf("translations=%d edits=%#v; want one translation of the final content", translations, edits)
	}
}

func TestHumanEditIsTranslatedImmediately(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})

	if err := service.ScheduleMessageUpdate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u", Content: "after",
	}); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookEdits) != 1 {
		t.Fatalf("human edit was not translated immediately: %#v", discord.webhookEdits)
	}
}
