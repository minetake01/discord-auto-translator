package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateGroupDefaultAndDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "c1", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "general", GuildID: "g1", DisplayName: "general", CreatedBy: "u1"}, GroupChannel{
		GroupID: "general", GuildID: "g1", ChannelID: "c2", ChannelType: 0, Language: "en", WebhookID: "w2", WebhookToken: "t2",
	})
	if !errors.Is(err, ErrDuplicateGroup) {
		t.Fatalf("want ErrDuplicateGroup, got %v", err)
	}
}

func TestJoinChannelRejectsMissingGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "missing", GuildID: "g1", ChannelID: "c1", Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	})
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("want ErrGroupNotFound, got %v", err)
	}
}

func TestJoinChannelRejectsDuplicateLanguageAndChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "c2", Language: "ja", WebhookID: "w2", WebhookToken: "t2"}); err == nil {
		t.Fatal("expected duplicate language error")
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "en", WebhookID: "w3", WebhookToken: "t3"}); err == nil {
		t.Fatal("expected duplicate channel error")
	}
}

func TestGroupAutocompleteQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"general", "support"} {
		err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: id, GuildID: "g1", DisplayName: id, CreatedBy: "u1"}, GroupChannel{
			GroupID: id, GuildID: "g1", ChannelID: id + "-c", Language: id + "-lang", WebhookID: "w", WebhookToken: "t",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Groups(ctx, "g1", "gen", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "general" {
		t.Fatalf("unexpected groups: %#v", got)
	}
}

func TestLeaveChannelRemovesChannelAndRelatedLinks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "c2", Language: "en", WebhookID: "w2", WebhookToken: "t2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "m1", SourceChannelID: "c1", GroupID: "team", TargetChannelID: "c2", TargetMessageID: "m2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThreadLink(ctx, ThreadLink{GroupID: "team", SourceThreadID: "th1", SourceChannelID: "c1", TargetThreadID: "th2", TargetChannelID: "c2"}); err != nil {
		t.Fatal(err)
	}

	if err := s.LeaveChannel(ctx, "g1", "team", "c2"); err != nil {
		t.Fatal(err)
	}

	channels, err := s.ChannelsInGroup(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].ChannelID != "c1" {
		t.Fatalf("unexpected channels: %#v", channels)
	}
	links, err := s.MessageTargets(ctx, "c1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("message links were not removed: %#v", links)
	}
	threads, err := s.SourceThreadTargets(ctx, "th1")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Fatalf("thread links were not removed: %#v", threads)
	}
}

func TestDeleteGroupRemovesGroupChannelsAndLinks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "c1", Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "m1", SourceChannelID: "c1", GroupID: "team", TargetChannelID: "c2", TargetMessageID: "m2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThreadLink(ctx, ThreadLink{GroupID: "team", SourceThreadID: "th1", SourceChannelID: "c1", TargetThreadID: "th2", TargetChannelID: "c2"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteGroup(ctx, "g1", "team"); err != nil {
		t.Fatal(err)
	}

	groups, err := s.Groups(ctx, "g1", "team", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("group was not deleted: %#v", groups)
	}
	channels, err := s.ChannelsInGroup(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 {
		t.Fatalf("channels were not deleted: %#v", channels)
	}
	links, err := s.MessageTargets(ctx, "c1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("message links were not removed: %#v", links)
	}
}

func TestRecentMessageHistoryReturnsUniquePreviousMessagesOldestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, link := range []MessageLink{
		{SourceMessageID: "100", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en", SourceAuthorID: "a", SourceContentSnapshot: "first"},
		{SourceMessageID: "101", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "201", TargetLanguage: "en", SourceAuthorID: "b", SourceContentSnapshot: "second"},
		{SourceMessageID: "101", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "fr", TargetMessageID: "301", TargetLanguage: "fr", SourceAuthorID: "b", SourceContentSnapshot: "second"},
		{SourceMessageID: "102", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "202", TargetLanguage: "en", SourceAuthorID: "c", SourceContentSnapshot: "current"},
	} {
		if err := s.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.RecentMessageHistory(ctx, []string{"ja"}, "102", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("history: %#v", got)
	}
	if got[0].SourceMessageID != "100" || got[1].SourceMessageID != "101" {
		t.Fatalf("unexpected order: %#v", got)
	}
}

func TestRecentMessageHistoryAggregatesAcrossChannels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, link := range []MessageLink{
		{SourceMessageID: "100", SourceChannelID: "en", GroupID: "team", TargetChannelID: "ja", TargetMessageID: "200", TargetLanguage: "ja", SourceAuthorID: "a", SourceContentSnapshot: "english first"},
		{SourceMessageID: "101", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "201", TargetLanguage: "en", SourceAuthorID: "b", SourceContentSnapshot: "japanese second"},
		{SourceMessageID: "102", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "202", TargetLanguage: "en", SourceAuthorID: "c", SourceContentSnapshot: "current"},
	} {
		if err := s.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.RecentMessageHistory(ctx, []string{"ja", "en"}, "102", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("history: %#v", got)
	}
	if got[0].SourceMessageID != "100" || got[0].SourceChannelID != "en" || got[1].SourceMessageID != "101" {
		t.Fatalf("unexpected order: %#v", got)
	}
}

func TestMarkProcessedIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first, err := s.MarkProcessed(ctx, "event-1")
	if err != nil {
		t.Fatal(err)
	}
	if !first {
		t.Fatal("expected first mark to succeed")
	}
	second, err := s.MarkProcessed(ctx, "event-1")
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("expected second mark to be ignored")
	}
	processed, err := s.IsEventProcessed(ctx, "event-1")
	if err != nil {
		t.Fatal(err)
	}
	if !processed {
		t.Fatal("expected event to be recorded")
	}
}

func TestGlossaryCRUDAndLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertGlossaryEntry(ctx, "g1", "NPC", "Non-Player Character", "略語", "u1", true); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListGlossaryEntries(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].SourceTerm != "NPC" || entries[0].Attribute != "略語" || !entries[0].AlwaysInclude {
		t.Fatalf("got %#v", entries)
	}
	if err := s.UpsertGlossaryEntry(ctx, "g1", "npc", "Updated", "自由入力の属性", "u2", false); err != nil {
		t.Fatal(err)
	}
	entries, err = s.ListGlossaryEntries(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].PreferredTranslation != "Updated" || entries[0].Attribute != "自由入力の属性" || entries[0].AlwaysInclude {
		t.Fatalf("expected upsert overwrite, got %#v", entries)
	}
	for i := 0; i < glossaryMaxEntries-1; i++ {
		if err := s.UpsertGlossaryEntry(ctx, "g1", fmt.Sprintf("term%d", i), "value", "", "u", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertGlossaryEntry(ctx, "g1", "overflow", "value", "", "u", false); !errors.Is(err, ErrGlossaryFull) {
		t.Fatalf("want ErrGlossaryFull, got %v", err)
	}
	if err := s.UpsertGlossaryEntry(ctx, "g1", "NPC", "Updated at capacity", "人名", "u", true); err != nil {
		t.Fatalf("update at capacity: %v", err)
	}
	if err := s.RemoveGlossaryEntry(ctx, "g1", "term0"); err != nil {
		t.Fatal(err)
	}
}

func TestGlossaryMigrationDefaultsNewFields(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE glossary_entries (
		guild_id TEXT NOT NULL, source_term TEXT NOT NULL, source_term_key TEXT NOT NULL,
		preferred_translation TEXT NOT NULL, created_by TEXT NOT NULL, created_at TEXT NOT NULL,
		PRIMARY KEY (guild_id, source_term_key)
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO glossary_entries VALUES ('g1','NPC','npc','Non-Player Character','u1','now')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListGlossaryEntries(ctx, "g1")
	if err != nil || len(entries) != 1 || entries[0].Attribute != "" || entries[0].AlwaysInclude {
		t.Fatalf("entries = %#v, err = %v", entries, err)
	}
}

func TestPinStateAndSnapshotUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	pinned, known, err := s.GetPinState(ctx, "ja", "msg")
	if err != nil || known || pinned {
		t.Fatalf("unexpected initial pin state: pinned=%v known=%v err=%v", pinned, known, err)
	}
	if err := s.SavePinState(ctx, "ja", "msg", true); err != nil {
		t.Fatal(err)
	}
	pinned, known, err = s.GetPinState(ctx, "ja", "msg")
	if err != nil || !known || !pinned {
		t.Fatalf("expected pinned state, got pinned=%v known=%v err=%v", pinned, known, err)
	}
	if err := s.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "msg", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "target", TargetLanguage: "en",
		SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMessageLinkSnapshot(ctx, "ja", "msg", "en", "after"); err != nil {
		t.Fatal(err)
	}
	links, err := s.MessageTargets(ctx, "ja", "msg")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].SourceContentSnapshot != "after" {
		t.Fatalf("snapshot not updated: %#v", links)
	}
}

func TestMessageOriginal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	link := MessageLink{
		SourceMessageID: "orig", SourceChannelID: "ja", GroupID: "team",
		TargetChannelID: "en", TargetMessageID: "translated", TargetLanguage: "en",
		SourceAuthorID: "a", SourceContentSnapshot: "hello",
	}
	if err := s.SaveMessageLink(ctx, link); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.MessageOriginal(ctx, "en", "translated")
	if err != nil || !ok {
		t.Fatalf("reverse lookup failed: %#v ok=%v err=%v", got, ok, err)
	}
	if got.SourceChannelID != "ja" || got.SourceMessageID != "orig" || got.Snapshot != "hello" || got.TargetLanguage != "en" || got.IsSource {
		t.Fatalf("unexpected reverse result: %#v", got)
	}

	got, ok, err = s.MessageOriginal(ctx, "ja", "orig")
	if err != nil || !ok {
		t.Fatalf("source lookup failed: %#v ok=%v err=%v", got, ok, err)
	}
	if !got.IsSource || got.SourceChannelID != "ja" || got.SourceMessageID != "orig" || got.Snapshot != "hello" {
		t.Fatalf("unexpected source result: %#v", got)
	}

	_, ok, err = s.MessageOriginal(ctx, "ja", "unknown")
	if err != nil || ok {
		t.Fatalf("want not found, got ok=%v err=%v", ok, err)
	}
}

func TestSetGroupStyleAndGroupStyle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "ch-ja", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}

	preset, custom, err := s.GroupStyle(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if preset != "" || custom != "" {
		t.Fatalf("default style = preset %q custom %q", preset, custom)
	}

	if err := s.SetGroupStyle(ctx, "g1", "team", "formal", ""); err != nil {
		t.Fatal(err)
	}
	preset, custom, err = s.GroupStyle(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if preset != "formal" || custom != "" {
		t.Fatalf("formal style = preset %q custom %q", preset, custom)
	}

	if err := s.SetGroupStyle(ctx, "g1", "team", "", "短くカジュアルに"); err != nil {
		t.Fatal(err)
	}
	preset, custom, err = s.GroupStyle(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if preset != "" || custom != "短くカジュアルに" {
		t.Fatalf("custom style = preset %q custom %q", preset, custom)
	}

	if err := s.SetGroupStyle(ctx, "g1", "team", "", ""); err != nil {
		t.Fatal(err)
	}
	preset, custom, err = s.GroupStyle(ctx, "g1", "team")
	if err != nil {
		t.Fatal(err)
	}
	if preset != "" || custom != "" {
		t.Fatalf("reset style = preset %q custom %q", preset, custom)
	}

	if err := s.SetGroupStyle(ctx, "g1", "missing", "formal", ""); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("SetGroupStyle missing group = %v, want ErrGroupNotFound", err)
	}
	_, _, err = s.GroupStyle(ctx, "g1", "missing")
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("GroupStyle missing group = %v, want ErrGroupNotFound", err)
	}
}
