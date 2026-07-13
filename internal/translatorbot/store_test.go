package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
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

func TestStoreOptimizedSchema(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	columnTypes := map[string]map[string]string{
		"translation_groups": {"created_at": "INTEGER"},
		"message_links":      {"source_message_id": "INTEGER"},
		"pin_states":         {"message_id": "INTEGER"},
		"processed_events":   {"created_at": "INTEGER"},
		"glossary_entries":   {"created_at": "INTEGER"},
	}
	for table, expected := range columnTypes {
		rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]string{}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, declaredType string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &declaredType, &notNull, &defaultValue, &primaryKey); err != nil {
				t.Fatal(err)
			}
			found[name] = declaredType
		}
		_ = rows.Close()
		for column, want := range expected {
			if found[column] != want {
				t.Errorf("%s.%s type = %q, want %q", table, column, found[column], want)
			}
		}
	}

	wantIndexes := []string{
		"idx_group_channels_guild_channel",
		"idx_message_links_source_channel_message",
		"idx_message_links_target_channel_message",
		"idx_message_links_group_source_channel",
		"idx_message_links_group_target_channel",
		"idx_thread_links_source_thread",
		"idx_thread_links_target_thread",
		"idx_thread_links_group_target_thread",
		"idx_thread_links_group_source_channel",
		"idx_thread_links_group_target_channel",
		"idx_pin_states_message",
		"idx_processed_events_created_at",
	}
	for _, index := range wantIndexes {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, index).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("index %s count = %d, want 1", index, count)
		}
	}
}

func TestStoreRejectsInvalidIntegerSnowflakes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "not-a-snowflake"}); err == nil {
		t.Fatal("SaveMessageLink accepted a non-numeric source message ID")
	}
	if err := s.SavePinState(ctx, "channel", "0", true); err == nil {
		t.Fatal("SavePinState accepted a non-positive message ID")
	}
}

func TestPurgeQueriesUseIndexesWithoutCast(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, tc := range []struct {
		query string
		index string
	}{
		{`DELETE FROM message_links WHERE source_message_id < ?`, "sqlite_autoindex_message_links_1"},
		{`DELETE FROM pin_states WHERE message_id < ?`, "idx_pin_states_message"},
		{`DELETE FROM processed_events WHERE created_at < ?`, "idx_processed_events_created_at"},
	} {
		rows, err := s.db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+tc.query, int64(1))
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		_ = rows.Close()
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, tc.index) || strings.Contains(strings.ToUpper(plan), "CAST(") {
			t.Errorf("query plan for %q = %q, want index %q without CAST", tc.query, plan, tc.index)
		}
	}
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
	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "100000000000000013", SourceChannelID: "c1", GroupID: "team", TargetChannelID: "c2", TargetMessageID: "m2"}); err != nil {
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
	links, err := s.MessageTargets(ctx, "c1", "100000000000000013")
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
	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "100000000000000013", SourceChannelID: "c1", GroupID: "team", TargetChannelID: "c2", TargetMessageID: "m2"}); err != nil {
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
	links, err := s.MessageTargets(ctx, "c1", "100000000000000013")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("message links were not removed: %#v", links)
	}
}

func TestDeleteMessageDataRemovesAllTargetsForSource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, link := range []MessageLink{
		{SourceMessageID: "100", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en", SourceAuthorID: "a", SourceContentSnapshot: "100000000000000021"},
		{SourceMessageID: "100", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "fr", TargetMessageID: "300", TargetLanguage: "fr", SourceAuthorID: "a", SourceContentSnapshot: "100000000000000021"},
		{SourceMessageID: "101", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "201", TargetLanguage: "en", SourceAuthorID: "b", SourceContentSnapshot: "second"},
	} {
		if err := s.SaveMessageLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.DeleteMessageData(ctx, "ja", "100", nil); err != nil {
		t.Fatal(err)
	}

	links, err := s.MessageTargets(ctx, "ja", "100")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("deleted source links remain: %#v", links)
	}
	remaining, err := s.MessageTargets(ctx, "ja", "101")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("other source links were removed: %#v", remaining)
	}
}

func TestMessageTargetsReplyingToReturnsPersistedReplyTargets(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "team",
		TargetChannelID: "en", TargetMessageID: "target-reply", TargetLanguage: "en",
		ReferencedMessageID: "100000000000000001", ReferencedChannelID: "ja",
	}); err != nil {
		t.Fatal(err)
	}

	links, err := s.MessageTargetsReplyingTo(ctx, "ja", "100000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].SourceMessageID != "100000000000000002" || links[0].TargetMessageID != "target-reply" {
		t.Fatalf("unexpected reply targets: %#v", links)
	}
}

func TestRecentMessageHistoryReturnsUniquePreviousMessagesOldestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, link := range []MessageLink{
		{SourceMessageID: "100", SourceChannelID: "ja", GroupID: "team", TargetChannelID: "en", TargetMessageID: "200", TargetLanguage: "en", SourceAuthorID: "a", SourceContentSnapshot: "100000000000000021"},
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

func TestPinStateAndSnapshotUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	pinned, known, err := s.GetPinState(ctx, "ja", "100000000000000014")
	if err != nil || known || pinned {
		t.Fatalf("unexpected initial pin state: pinned=%v known=%v err=%v", pinned, known, err)
	}
	if err := s.SavePinState(ctx, "ja", "100000000000000014", true); err != nil {
		t.Fatal(err)
	}
	pinned, known, err = s.GetPinState(ctx, "ja", "100000000000000014")
	if err != nil || !known || !pinned {
		t.Fatalf("expected pinned state, got pinned=%v known=%v err=%v", pinned, known, err)
	}
	if err := s.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000014", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "target", TargetLanguage: "en",
		SourceContentSnapshot: "before",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMessageLinkSnapshot(ctx, "ja", "100000000000000014", "en", "after"); err != nil {
		t.Fatal(err)
	}
	links, err := s.MessageTargets(ctx, "ja", "100000000000000014")
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
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "team",
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
	if got.SourceChannelID != "ja" || got.SourceMessageID != "100000000000000002" || got.Snapshot != "hello" || got.TargetLanguage != "en" || got.IsSource {
		t.Fatalf("unexpected reverse result: %#v", got)
	}

	got, ok, err = s.MessageOriginal(ctx, "ja", "100000000000000002")
	if err != nil || !ok {
		t.Fatalf("source lookup failed: %#v ok=%v err=%v", got, ok, err)
	}
	if !got.IsSource || got.SourceChannelID != "ja" || got.SourceMessageID != "100000000000000002" || got.Snapshot != "hello" {
		t.Fatalf("unexpected source result: %#v", got)
	}

	_, ok, err = s.MessageOriginal(ctx, "ja", "100000000000000011")
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

func TestPurgeMessageLinksOlderThan(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	cutoff := now.Add(-30 * 24 * time.Hour)
	oldID := snowflakeForTime(now.Add(-31*24*time.Hour), 1)
	newID := snowflakeForTime(now.Add(-29*24*time.Hour), 2)

	for _, link := range []struct {
		id, target string
	}{
		{oldID, "target-old"},
		{newID, "target-new"},
	} {
		if err := s.SaveMessageLink(ctx, MessageLink{
			SourceMessageID: link.id, SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: link.target, TargetLanguage: "en",
			SourceAuthorID: "u", SourceContentSnapshot: link.id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{oldID, newID} {
		if err := s.SavePinState(ctx, "ja", id, true); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.PurgeMessageLinksOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d links, want 1", n)
	}

	oldLinks, err := s.MessageTargets(ctx, "ja", oldID)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldLinks) != 0 {
		t.Fatalf("old link should be purged: %#v", oldLinks)
	}
	newLinks, err := s.MessageTargets(ctx, "ja", newID)
	if err != nil {
		t.Fatal(err)
	}
	if len(newLinks) != 1 {
		t.Fatalf("new link should remain: %#v", newLinks)
	}
	_, oldKnown, err := s.GetPinState(ctx, "ja", oldID)
	if err != nil || oldKnown {
		t.Fatalf("old pin should be purged: known=%v err=%v", oldKnown, err)
	}
	_, newKnown, err := s.GetPinState(ctx, "ja", newID)
	if err != nil || !newKnown {
		t.Fatalf("new pin should remain: known=%v err=%v", newKnown, err)
	}
}
