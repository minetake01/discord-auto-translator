package translatorbot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func smallMirrorLinks(t *testing.T, store *Store, sourceChannelID, sourceMessageID string) []MessageLink {
	t.Helper()
	links, err := store.MessageTargets(context.Background(), sourceChannelID, sourceMessageID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("links = %#v, want one", links)
	}
	return links
}

func TestHandleMessageCreateSplitsLongMirror(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	source := strings.Repeat("a", 2500)
	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", AuthorAvatarURL: "https://cdn.example/avatar.png", AuthorRoleColor: 0x123456,
		Content: source,
	}); err != nil {
		t.Fatal(err)
	}

	if len(discord.sent) != 2 {
		t.Fatalf("sent %d messages, want two: %#v", len(discord.sent), discord.sent)
	}
	want := "[en] " + source
	if got := discord.sent[0].Content + discord.sent[1].Content; got != want {
		t.Fatalf("split content does not reassemble: got %d units", discordContentLength(got))
	}
	for i, send := range discord.sent {
		if discordContentLength(send.Content) > discordMessageContentLimit {
			t.Fatalf("part %d exceeds the limit: %d", i, discordContentLength(send.Content))
		}
		if send.Username != "author" || send.AvatarURL != "https://cdn.example/avatar.png" {
			t.Fatalf("part %d changed the webhook identity: %#v", i, send)
		}
	}
	if len(discord.sent[1].Files) != 0 || len(discord.sent[1].Embeds) != 0 {
		t.Fatalf("continuation carried files or embeds: %#v", discord.sent[1])
	}

	link := smallMirrorLinks(t, store, "ja", "100000000000000001")[0]
	wantContinuations := []string{fakeSentID(2)}
	if len(link.ContinuationMessageIDs) != 1 || link.ContinuationMessageIDs[0] != wantContinuations[0] {
		t.Fatalf("continuations = %#v, want %#v", link.ContinuationMessageIDs, wantContinuations)
	}
	if link.SourceAuthorAvatarURL != "https://cdn.example/avatar.png" || link.SourceAuthorRoleColor != 0x123456 {
		t.Fatalf("author identity not stored: %#v", link)
	}
}

func TestHandleMessageCreateRemovesPostedPartsWhenAContinuationFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	calls := 0
	discord.sendErr = func(WebhookSend) error {
		calls++
		if calls == 2 {
			return errors.New("continuation failed")
		}
		return nil
	}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", Content: strings.Repeat("a", 2500),
	})
	if err == nil {
		t.Fatal("expected the continuation failure to surface")
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent = %#v, want only the primary", discord.sent)
	}
	if len(discord.webhookDeletes) != 1 || discord.webhookDeletes[0].messageID != fakeSentID(1) {
		t.Fatalf("compensating deletes = %#v", discord.webhookDeletes)
	}
	if links, err := store.MessageTargets(ctx, "ja", "100000000000000001"); err != nil || len(links) != 0 {
		t.Fatalf("links after failed continuation = %#v err=%v", links, err)
	}
}

func TestSaveMessageLinkReplacesContinuations(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	link := MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "900000000000000001", TargetLanguage: "en",
		ContinuationMessageIDs: []string{"900000000000000002", "900000000000000003"},
		SourceAuthorID:         "u", SourceContentSnapshot: "long",
	}
	if err := store.SaveMessageLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	link.ContinuationMessageIDs = []string{"900000000000000004"}
	if err := store.SaveMessageLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	got := smallMirrorLinks(t, store, "ja", link.SourceMessageID)[0].ContinuationMessageIDs
	if len(got) != 1 || got[0] != "900000000000000004" {
		t.Fatalf("continuations = %#v", got)
	}
}

func TestReplyToContinuationPartQuotesTheOrigin(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	referenced := DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", Content: "first line\n" + strings.Repeat("a", 2500),
	}
	if err := service.HandleMessageCreate(ctx, referenced); err != nil {
		t.Fatal(err)
	}
	link := smallMirrorLinks(t, store, "ja", referenced.ID)[0]
	if len(link.ContinuationMessageIDs) != 1 {
		t.Fatalf("referenced mirror was not split: %#v", link)
	}
	continuationID := link.ContinuationMessageIDs[0]

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000002", ChannelID: "en", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", Content: "reply body",
		ReferencedMessageID: continuationID, ReferencedMessageChannelID: "en",
	}); err != nil {
		t.Fatal(err)
	}
	reply := discord.sent[len(discord.sent)-1]
	want := "> -# first line · [引用元を見る](https://discord.com/channels/guild/ja/100000000000000001)\n\n[ja] reply body"
	if reply.Content != want {
		t.Fatalf("reply quote = %q, want %q", reply.Content, want)
	}

	// A reply in the target channel references the continuation part, so
	// editing the original must still find and refresh that reply's quote.
	replyLink := smallMirrorLinks(t, store, "en", "100000000000000002")[0]
	referenced.Content = "changed line\n" + strings.Repeat("a", 2500)
	if err := service.HandleMessageUpdate(ctx, referenced); err != nil {
		t.Fatal(err)
	}
	lastEdit := discord.webhookEdits[len(discord.webhookEdits)-1]
	if lastEdit.messageID != replyLink.TargetMessageID || !strings.HasPrefix(lastEdit.content, "> -# changed line") {
		t.Fatalf("reply quote was not refreshed through the continuation reference: %#v", lastEdit)
	}
}

func TestHandleMessageUpdateGrowsAndShrinksMirrorParts(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	long := strings.Repeat("a", 2500)
	msg := DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", Content: long,
	}
	if err := service.HandleMessageCreate(ctx, msg); err != nil {
		t.Fatal(err)
	}
	primaryID := fakeSentID(1)
	continuationID := fakeSentID(2)

	// The edit fits in one message: the continuation is deleted.
	msg.Content = "short"
	if err := service.HandleMessageUpdate(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if got := discord.webhookEdits[len(discord.webhookEdits)-1]; got.messageID != primaryID || got.content != "[en] short" {
		t.Fatalf("shrink edit = %#v", got)
	}
	if len(discord.webhookDeletes) != 1 || discord.webhookDeletes[0].messageID != continuationID {
		t.Fatalf("shrink deletes = %#v, want %s", discord.webhookDeletes, continuationID)
	}
	if link := smallMirrorLinks(t, store, "ja", msg.ID)[0]; len(link.ContinuationMessageIDs) != 0 {
		t.Fatalf("continuations after shrink = %#v", link.ContinuationMessageIDs)
	}

	// Growing again posts a new continuation and records it.
	msg.Content = long
	if err := service.HandleMessageUpdate(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if len(discord.sent) != 3 {
		t.Fatalf("sent = %#v, want a new continuation", discord.sent)
	}
	newContinuation := fakeSentID(3)
	wantContinuation := splitMessageContent("[en] "+long, discordMessageContentLimit)[1]
	if got := discord.sent[2].Content; got != wantContinuation {
		t.Fatalf("new continuation = %q, want %q", got, wantContinuation)
	}
	link := smallMirrorLinks(t, store, "ja", msg.ID)[0]
	if len(link.ContinuationMessageIDs) != 1 || link.ContinuationMessageIDs[0] != newContinuation {
		t.Fatalf("continuations after grow = %#v, want %s", link.ContinuationMessageIDs, newContinuation)
	}
}

func TestHandleMessageDeleteDeletesEveryMirrorPart(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	msg := DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", Content: strings.Repeat("a", 2500),
	}
	if err := service.HandleMessageCreate(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleMessageDelete(ctx, "guild", "ja", msg.ID); err != nil {
		t.Fatal(err)
	}
	if len(discord.webhookDeletes) != 2 {
		t.Fatalf("deletes = %#v, want both parts", discord.webhookDeletes)
	}
	if discord.webhookDeletes[0].messageID != fakeSentID(1) || discord.webhookDeletes[1].messageID != fakeSentID(2) {
		t.Fatalf("unexpected delete order: %#v", discord.webhookDeletes)
	}
	if links, err := store.MessageTargets(ctx, "ja", msg.ID); err != nil || len(links) != 0 {
		t.Fatalf("links after delete = %#v err=%v", links, err)
	}
}

func TestHandleMessageUpdateRefreshesReplyQuoteWithoutTouchingContinuations(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	referenced := DiscordMessage{
		ID: "100000000000000001", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", Content: "first line\n" + strings.Repeat("a", 2500),
	}
	if err := service.HandleMessageCreate(ctx, referenced); err != nil {
		t.Fatal(err)
	}
	reply := DiscordMessage{
		ID: "100000000000000002", ChannelID: "ja", GuildID: "guild", AuthorID: "u",
		AuthorDisplayName: "author", Content: strings.Repeat("b", 2500),
		ReferencedMessageID: referenced.ID, ReferencedMessageChannelID: "ja",
		ReferencedMessageContent: referenced.Content,
	}
	if err := service.HandleMessageCreate(ctx, reply); err != nil {
		t.Fatal(err)
	}
	replyLinks := smallMirrorLinks(t, store, "ja", reply.ID)
	replyPrimary := replyLinks[0].TargetMessageID
	replyContinuation := replyLinks[0].ContinuationMessageIDs[0]
	if replyPrimary != fakeSentID(3) || replyContinuation != fakeSentID(4) {
		t.Fatalf("reply mirror = %#v", replyLinks[0])
	}
	sendsBefore := len(discord.sent)

	referenced.Content = "second line\n" + strings.Repeat("a", 2500)
	if err := service.HandleMessageUpdate(ctx, referenced); err != nil {
		t.Fatal(err)
	}

	if len(discord.sent) != sendsBefore {
		t.Fatalf("quote refresh posted new messages: %#v", discord.sent[sendsBefore:])
	}
	if len(discord.webhookDeletes) != 0 {
		t.Fatalf("quote refresh deleted messages: %#v", discord.webhookDeletes)
	}
	var quoteEdit *webhookEditCall
	for i := range discord.webhookEdits {
		if discord.webhookEdits[i].messageID == replyPrimary {
			quoteEdit = &discord.webhookEdits[i]
		}
		if discord.webhookEdits[i].messageID == replyContinuation {
			t.Fatalf("quote refresh edited a continuation part: %#v", discord.webhookEdits[i])
		}
	}
	if quoteEdit == nil {
		t.Fatalf("reply primary was not edited: %#v", discord.webhookEdits)
	}
	if !strings.HasPrefix(quoteEdit.content, "> -# [en] second line") {
		t.Fatalf("quote was not refreshed: %q", quoteEdit.content)
	}
	link := smallMirrorLinks(t, store, "ja", reply.ID)[0]
	if len(link.ContinuationMessageIDs) != 1 || link.ContinuationMessageIDs[0] != replyContinuation {
		t.Fatalf("continuations changed: %#v", link.ContinuationMessageIDs)
	}
}

func seedForumGroup(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "forum-ja", ChannelType: int(discordgo.ChannelTypeGuildForum),
		Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "forum-en", ChannelType: int(discordgo.ChannelTypeGuildForum),
		Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSyncThreadCreateSplitsLongForumInitialMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedForumGroup(t, store)

	source := strings.Repeat("a", 2500)
	req := threadCreateRequest{
		GuildID: "guild", SourceChannelID: "forum-ja", SourceThreadID: "100000000000000100",
		SourceMessageID: "100000000000000100", Name: "topic",
		InitialMessageID: "100000000000000100", InitialMessageAuthor: "u", InitialMessageUsername: "author",
		InitialMessageAvatarURL: "https://cdn.example/avatar.png", InitialMessageText: source,
	}
	created, err := service.syncThreadCreate(ctx, req, liveTranslation)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("thread should have been created with its initial message")
	}
	if len(discord.threads) != 1 {
		t.Fatalf("threads = %#v", discord.threads)
	}
	threadID := fakeThreadID(1)
	if discord.threads[0].content != "[en] "+source[:2000-len("[en] ")] {
		t.Fatalf("initial message was not the first part: %d units", discordContentLength(discord.threads[0].content))
	}
	if len(discord.sent) != 1 {
		t.Fatalf("sent = %#v, want one continuation", discord.sent)
	}
	continuation := discord.sent[0]
	if continuation.ThreadID != threadID || continuation.Username != "author" || continuation.AvatarURL != "https://cdn.example/avatar.png" {
		t.Fatalf("continuation = %#v", continuation)
	}
	if got := discord.threads[0].content + continuation.Content; got != "[en] "+source {
		t.Fatalf("forum initial message does not reassemble: %d units", discordContentLength(got))
	}
	links := smallMirrorLinks(t, store, req.SourceThreadID, req.InitialMessageID)
	if len(links[0].ContinuationMessageIDs) != 1 || links[0].ContinuationMessageIDs[0] != fakeSentID(2) {
		t.Fatalf("continuations = %#v", links[0].ContinuationMessageIDs)
	}
}

func TestMessageLinkPartsResolveAndCascade(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	link := MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "900000000000000001", TargetLanguage: "en",
		ContinuationMessageIDs: []string{"900000000000000002", "900000000000000003"},
		SourceAuthorID:         "u", SourceContentSnapshot: "long",
	}
	if err := store.SaveMessageLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "fr", TargetMessageID: "900000000000000010", TargetLanguage: "fr",
		ContinuationMessageIDs: []string{"900000000000000011"},
		SourceAuthorID:         "u", SourceContentSnapshot: "long",
	}); err != nil {
		t.Fatal(err)
	}

	links, err := store.MessageTargets(ctx, "ja", link.SourceMessageID)
	if err != nil || len(links) != 2 {
		t.Fatalf("links = %#v err=%v", links, err)
	}
	var english MessageLink
	for _, candidate := range links {
		if candidate.TargetChannelID == "en" {
			english = candidate
		}
	}
	if got := english.ContinuationMessageIDs; len(got) != 2 || got[0] != "900000000000000002" || got[1] != "900000000000000003" {
		t.Fatalf("continuations = %#v", got)
	}

	original, ok, err := store.MessageOriginal(ctx, "en", "900000000000000003")
	if err != nil || !ok || original.SourceMessageID != link.SourceMessageID || original.TargetLanguage != "en" {
		t.Fatalf("original from continuation = %#v ok=%v err=%v", original, ok, err)
	}
	_, quoteChannelID, quoteMessageID, ok, err := store.MessageQuoteTarget(ctx, "en", "900000000000000003", "en")
	if err != nil || !ok || quoteChannelID != "en" || quoteMessageID != link.TargetMessageID {
		t.Fatalf("quote from continuation = %s/%s ok=%v err=%v", quoteChannelID, quoteMessageID, ok, err)
	}
	peers, err := store.MessagePeers(ctx, "en", "900000000000000002")
	if err != nil {
		t.Fatal(err)
	}
	foundPeer := false
	for _, peer := range peers {
		if peer.TargetChannelID == "fr" && peer.TargetMessageID == "900000000000000010" {
			foundPeer = true
		}
	}
	if !foundPeer {
		t.Fatalf("peers of a continuation = %#v", peers)
	}

	if err := store.UpdateMessageLinkContinuations(ctx, link.SourceChannelID, link.SourceMessageID, link.TargetChannelID, []string{"900000000000000004"}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.MessageTargets(ctx, "ja", link.SourceMessageID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range updated {
		if candidate.TargetChannelID == "en" {
			if got := candidate.ContinuationMessageIDs; len(got) != 1 || got[0] != "900000000000000004" {
				t.Fatalf("updated continuations = %#v", got)
			}
		}
	}

	if err := store.DeleteMessageData(ctx, link.SourceChannelID, link.SourceMessageID, links); err != nil {
		t.Fatal(err)
	}
	var parts int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_link_parts`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if parts != 0 {
		t.Fatalf("continuation rows after delete = %d, want 0", parts)
	}
}

func TestMessageLinkContinuationRequiresSnowflake(t *testing.T) {
	store := newTestStore(t)
	err := store.SaveMessageLink(context.Background(), MessageLink{
		SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "900000000000000001", TargetLanguage: "en",
		ContinuationMessageIDs: []string{"not-a-snowflake"},
		SourceAuthorID:         "u", SourceContentSnapshot: "long",
	})
	if err == nil {
		t.Fatal("expected a non-snowflake continuation to be rejected")
	}
	if !strings.Contains(err.Error(), "continuation_message_id[0]") {
		t.Fatalf("unexpected error: %v", err)
	}
}
