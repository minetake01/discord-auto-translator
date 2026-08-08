package translatorbot

// SPEC 3.7

import (
	"context"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// SPEC 3.7
func TestSyncThreadCreateAndThreadMessage(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedGroup(t, store)

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000005", "topic", nil); err != nil {
		t.Fatal(err)
	}
	if len(discord.threads) != 1 || discord.threads[0].channelID != "en" || discord.threads[0].name != "[en] topic" {
		t.Fatalf("unexpected thread sync: %#v", discord.threads)
	}
	if len(translator.contexts) != 1 || translator.contexts[0].GuildID != "guild" || translator.contexts[0].MessageID != "100000000000000005" {
		t.Fatalf("thread name metadata context: %#v", translator.contexts)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000017", ChannelID: "100000000000000005", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
		AuthorID: "u", AuthorDisplayName: "u", Content: "スレッド本文",
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
	if len(translator.contexts) < 1 {
		t.Fatalf("contexts: %#v", translator.contexts)
	}
	var msgContext *TranslationContext
	for i := range translator.contexts {
		if translator.contexts[i].Author == "u" {
			msgContext = &translator.contexts[i]
			break
		}
	}
	if msgContext == nil || msgContext.GuildID != "guild" || msgContext.MessageID != "100000000000000017" || msgContext.ThreadName != "topic" {
		t.Fatalf("unexpected thread name in context: %#v", translator.contexts)
	}

	translatorCalls := len(translator.contexts)
	stubImageHTTP(service)
	err = service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000020", ChannelID: "100000000000000005", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u",
		Attachments: []DiscordAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/thread.png?ex=1", Filename: "thread.png", ContentType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != translatorCalls+1 {
		t.Fatal("image-only thread message must be translated")
	}
	if got := discord.sent[1]; got.ThreadID != "thread-1" || strings.TrimSpace(got.Content) != "" || len(got.Files) != 1 {
		t.Fatalf("unexpected attachment-only thread message: %#v", got)
	}

	err = service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000024", ChannelID: "100000000000000005", GuildID: "guild", AuthorID: "u", AuthorDisplayName: "u", Content: "`fmt.Println()`",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.contexts) != translatorCalls+1 {
		t.Fatal("code-only thread message must not be translated")
	}
	if got := discord.sent[2]; got.ThreadID != "thread-1" || got.Content != "`fmt.Println()`" {
		t.Fatalf("unexpected code-only thread message: %#v", got)
	}
}

// SPEC 3.7
func TestThreadStarterMessageIsSkippedWhenExistingMessageStartsThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000005", "topic", nil); err != nil {
		t.Fatal(err)
	}
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "starter", ChannelID: "100000000000000005", GuildID: "guild", AuthorID: "u",
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

// SPEC 3.7
func TestGatewayThreadCreateDefersUntilStarterWhenParentMessageIsNotLinked(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreateFromGateway(ctx, "guild", "ja", "100000000000000006", "topic", nil); err != nil {
		t.Fatal(err)
	}
	if len(discord.threads) != 0 {
		t.Fatalf("thread should wait for source message link: %#v", discord.threads)
	}

	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000015", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "本文",
	}); err != nil {
		t.Fatal(err)
	}
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "starter", ChannelID: "100000000000000006", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
		ReferencedMessageID: "100000000000000006", ThreadSystemMessage: true, ThreadStarterMessage: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.messageID != "100000000000000015" || got.name != "[en] topic" {
		t.Fatalf("unexpected thread sync: %#v", got)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("starter message should not be sent separately: %#v", discord.sent)
	}
}

// SPEC 3.7
func TestThreadMessageCreateSyncsThreadWhenMessageArrivesBeforeThreadCreate(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000021", ChannelID: "100000000000000005", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
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

// SPEC 3.7
func TestGatewayThreadCreateAndFirstThreadMessageDoNotDuplicateThread(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreateFromGateway(ctx, "guild", "ja", "100000000000000005", "topic", nil); err != nil {
		t.Fatal(err)
	}
	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000021", ChannelID: "100000000000000005", GuildID: "guild", ParentChannelID: "ja", ThreadName: "topic",
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

// SPEC 3.7
func TestSyncThreadCreateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000005", "topic", nil); err != nil {
		t.Fatal(err)
	}
	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000005", "topic", nil); err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("duplicate target threads were created: %#v", discord.threads)
	}
}

// SPEC 3.7
func TestSyncThreadCreateFromMessageUsesTranslatedMessageAndTitle(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000006", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "100000000000000015", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "本文",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000006", "議題", nil); err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.messageID != "100000000000000015" || got.name != "[en] 議題" {
		t.Fatalf("unexpected thread sync: %#v", got)
	}
}

// SPEC 3.7
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

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000007", "議題", nil); err != nil {
		t.Fatal(err)
	}

	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.channelType != int(discordgo.ChannelTypeGuildForum) || got.name != "[en] 議題" || got.content != "[en] 議題" {
		t.Fatalf("unexpected forum thread sync: %#v", got)
	}
}

// SPEC 3.7
func TestForumInitialMessageCreatesThreadWithTranslatedInitialContentAndLink(t *testing.T) {
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
		GroupID: "g", GuildID: "guild", ChannelID: "en", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}

	err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000007", ChannelID: "100000000000000007", GuildID: "guild", ParentChannelID: "ja", ThreadName: "議題",
		AuthorID: "u", AuthorDisplayName: "u", Content: "最初の本文",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("title and body should be one translation call: %#v", translator.contexts)
	}
	if translator.contexts[0].ThreadName != "" {
		t.Fatalf("thread create translation should not put the name in discord_context: %#v", translator.contexts[0])
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
	links, err := store.MessageTargets(ctx, "100000000000000007", "100000000000000007")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].TargetChannelID != "thread-1" || links[0].TargetMessageID != "thread-1" {
		t.Fatalf("unexpected forum starter message link: %#v", links)
	}
}

// SPEC 3.7
func TestSyncThreadUpdateRenamesTargetThreads(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000005", "topic", nil); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadUpdate(ctx, "guild", "100000000000000005", "新タイトル", nil, true, false); err != nil {
		t.Fatal(err)
	}

	if len(discord.edits) != 1 {
		t.Fatalf("edits: %#v", discord.edits)
	}
	if got := discord.edits[0]; got.threadID != "thread-1" || got.name != "[en] 新タイトル" {
		t.Fatalf("unexpected thread edit: %#v", got)
	}
}

// SPEC 3.7
func TestSyncThreadDeleteDeletesTargetThreadsAndLinks(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	service := NewService(store, discord, &echoTranslator{})
	seedGroup(t, store)
	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000005", "topic", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000017", SourceChannelID: "100000000000000005", GroupID: "g",
		TargetChannelID: "thread-1", TargetMessageID: "mirrored-msg", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "スレッド内メッセージ",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadDelete(ctx, "100000000000000005"); err != nil {
		t.Fatal(err)
	}

	if len(discord.deletes) != 1 || discord.deletes[0] != "thread-1" {
		t.Fatalf("deletes: %#v", discord.deletes)
	}
	threads, err := store.ThreadTargets(ctx, "100000000000000005")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Fatalf("thread links were not deleted: %#v", threads)
	}
	links, err := store.MessageTargets(ctx, "100000000000000005", "100000000000000017")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("thread message links were not deleted: %#v", links)
	}
}

// SPEC 3.7
func TestSyncThreadUpdateBatchesTranslationByGroup(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{}
	translator := &echoTranslator{}
	service := NewService(store, discord, translator)
	seedMultiLangGroup(t, store)
	for _, link := range []ThreadLink{
		{GroupID: "g", SourceThreadID: "100000000000000005", SourceChannelID: "ja", TargetThreadID: "thread-en", TargetChannelID: "en", TargetLanguage: "en"},
		{GroupID: "g", SourceThreadID: "100000000000000005", SourceChannelID: "ja", TargetThreadID: "thread-fr", TargetChannelID: "fr", TargetLanguage: "fr"},
	} {
		if err := store.SaveThreadLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	if err := service.SyncThreadUpdate(ctx, "guild", "100000000000000005", "新タイトル", nil, true, false); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("expected one batched translation call, got %#v", translator.contexts)
	}
	if len(discord.edits) != 2 {
		t.Fatalf("expected two thread edits, got %#v", discord.edits)
	}
}

// SPEC 3.7
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
		GroupID: "g", GuildID: "guild", ChannelID: "en", ChannelType: int(discordgo.ChannelTypeGuildForum), Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.HandleMessageCreate(ctx, DiscordMessage{
		ID: "100000000000000007", ChannelID: "100000000000000007", GuildID: "guild", ParentChannelID: "ja", ThreadName: "議題",
		AuthorID: "u", AuthorDisplayName: "u", Content: "<@123> `example` <:wave:456>",
	}); err != nil {
		t.Fatal(err)
	}

	if len(translator.contexts) != 1 {
		t.Fatalf("only the thread name should be translated: %#v", translator.contexts)
	}
	if translator.contexts[0].ThreadName != "" {
		t.Fatalf("thread create translation should not put the name in discord_context: %#v", translator.contexts[0])
	}
	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0]; got.channelID != "en" || got.name != "[en] 議題" || got.content != "<@123> `example` <:wave:456>" {
		t.Fatalf("unexpected forum thread sync: %#v", got)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("forum starter should not be sent as a second message: %#v", discord.sent)
	}
}

func TestSyncThreadCreateAppliesMappedForumTags(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		channels: map[string]*discordgo.Channel{
			"en": {ID: "en", Type: discordgo.ChannelTypeGuildForum},
		},
	}
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
	if err := store.UpsertForumTagMap(ctx, "guild", "g", "ja", "tag-ja", "en", "tag-en"); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000007", "議題", []string{"tag-ja", "unmapped"}); err != nil {
		t.Fatal(err)
	}
	if len(discord.threads) != 1 {
		t.Fatalf("threads: %#v", discord.threads)
	}
	if got := discord.threads[0].appliedTags; len(got) != 1 || got[0] != "tag-en" {
		t.Fatalf("applied tags: %#v", got)
	}
}

func TestSyncThreadCreateFailsWhenRequireTagAndUnmapped(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		channels: map[string]*discordgo.Channel{
			"en": {ID: "en", Type: discordgo.ChannelTypeGuildForum, Flags: discordgo.ChannelFlagRequireTag},
		},
	}
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

	err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000007", "議題", []string{"tag-ja"})
	if err == nil {
		t.Fatal("expected require-tag failure")
	}
}

func TestSyncThreadUpdateSyncsMappedForumTags(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	discord := &fakeDiscordAPI{
		channels: map[string]*discordgo.Channel{
			"en":       {ID: "en", Type: discordgo.ChannelTypeGuildForum},
			"thread-1": {ID: "thread-1", AppliedTags: []string{}},
		},
	}
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
	if err := store.UpsertForumTagMap(ctx, "guild", "g", "ja", "tag-ja", "en", "tag-en"); err != nil {
		t.Fatal(err)
	}
	if err := service.SyncThreadCreate(ctx, "guild", "ja", "100000000000000007", "topic", nil); err != nil {
		t.Fatal(err)
	}

	if err := service.SyncThreadUpdate(ctx, "guild", "100000000000000007", "topic", []string{"tag-ja"}, false, true); err != nil {
		t.Fatal(err)
	}
	if len(discord.edits) != 1 || discord.edits[0].appliedTags == nil {
		t.Fatalf("edits: %#v", discord.edits)
	}
	if got := *discord.edits[0].appliedTags; len(got) != 1 || got[0] != "tag-en" {
		t.Fatalf("edited tags: %#v", got)
	}
}
