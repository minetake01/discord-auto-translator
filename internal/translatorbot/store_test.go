package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreFileBackedConcurrentReaderDoesNotPoisonWrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "store.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.SaveMessageLink(ctx, MessageLink{SourceMessageID: "100000000000000000", SourceChannelID: "ja", GroupID: "g", TargetChannelID: "en", TargetMessageID: "200000000000000000", TargetLanguage: "en", SourceAuthorID: "u", SourceContentSnapshot: "seed"}); err != nil {
		t.Fatal(err)
	}

	readerHeld := make(chan struct{})
	releaseReader := make(chan struct{})
	readerDone := make(chan struct{})
	writeDone := make(chan error, 1)
	writeCtx, cancelWrite := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWrite()

	go func() {
		defer close(readerDone)
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Errorf("reader conn: %v", err)
			close(readerHeld)
			return
		}
		defer conn.Close()
		rows, err := conn.QueryContext(ctx, `SELECT source_message_id FROM message_links`)
		if err != nil {
			t.Errorf("reader query: %v", err)
			close(readerHeld)
			return
		}
		if !rows.Next() {
			t.Error("reader query returned no rows")
			_ = rows.Close()
			close(readerHeld)
			return
		}
		var sourceMessageID string
		if err := rows.Scan(&sourceMessageID); err != nil {
			t.Errorf("reader scan: %v", err)
			_ = rows.Close()
			close(readerHeld)
			return
		}
		close(readerHeld)
		<-releaseReader
		_ = rows.Close()
	}()

	<-readerHeld

	go func() {
		writeDone <- s.SaveMessageLink(writeCtx, MessageLink{
			SourceMessageID: "100000000000000001", SourceChannelID: "ja", GroupID: "g",
			TargetChannelID: "en", TargetMessageID: "200000000000000001", TargetLanguage: "en",
			SourceAuthorID: "u", SourceContentSnapshot: "first",
		})
	}()

	var prematureWrite error
	writeCompletedEarly := false
	select {
	case err := <-writeDone:
		writeCompletedEarly = true
		prematureWrite = err
	case <-time.After(200 * time.Millisecond):
	}

	close(releaseReader)
	<-readerDone

	if writeCompletedEarly {
		t.Fatalf("write completed while reader held connection: %v", prematureWrite)
	}

	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write after reader released: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("write timed out after reader released")
	}

	if err := s.SaveMessageLink(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "g",
		TargetChannelID: "en", TargetMessageID: "200000000000000002", TargetLanguage: "en",
		SourceAuthorID: "u", SourceContentSnapshot: "second",
	}); err != nil {
		t.Fatalf("subsequent write poisoned: %v", err)
	}
	links, err := s.MessageTargets(ctx, "ja", "100000000000000002")
	if err != nil {
		t.Fatalf("subsequent read poisoned: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("subsequent read: %#v", links)
	}
}

func TestGuildLifecycleWriteWaitsForTransientSQLiteContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "translator.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Close() })
	conn, err := locker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- s.MarkGuildRemoved(context.Background(), "guild", time.Unix(1, 0))
	}()
	select {
	case err := <-done:
		t.Fatalf("lifecycle write did not wait for SQLite lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if _, err := conn.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("lifecycle write did not recover from transient contention: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle write exceeded bounded SQLite busy timeout")
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM guild_removals WHERE guild_id='guild'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("persisted markers = %d, want 1", count)
	}
}

func TestMarkGuildRemovedUpsertsSingleMarker(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	second := first.Add(24 * time.Hour)

	if err := s.MarkGuildRemoved(ctx, "guild", first); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkGuildRemoved(ctx, "guild", second); err != nil {
		t.Fatal(err)
	}

	var count int
	var removedAt int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(removed_at) FROM guild_removals WHERE guild_id='guild'`).Scan(&count, &removedAt); err != nil {
		t.Fatal(err)
	}
	if count != 1 || removedAt != second.UnixMilli() {
		t.Fatalf("marker count=%d removed_at=%d, want 1 and %d", count, removedAt, second.UnixMilli())
	}
}

func TestCancelGuildRemovalDeletesMarker(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.MarkGuildRemoved(ctx, "guild", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if err := s.CancelGuildRemoval(ctx, "guild"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM guild_removals WHERE guild_id='guild'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("marker count = %d, want 0", count)
	}
}

func TestGuildIDsWithStoredDataReturnsDistinctSortedGuilds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	statements := []string{
		`INSERT INTO translation_groups(id,guild_id,display_name,created_by,created_at) VALUES('group','guild-b','B','admin',1)`,
		`INSERT INTO glossary_entries(guild_id,source_term,source_term_key,preferred_translation,created_by,created_at) VALUES('guild-a','term','term','value','admin',1)`,
		`INSERT INTO source_allowlists(guild_id,source_type,source_id,created_by,created_at) VALUES('guild-b','bot','123456789012345678','admin',1)`,
		`INSERT INTO guild_removals(guild_id,removed_at) VALUES('guild-c',1)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	guildIDs, err := s.GuildIDsWithStoredData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(guildIDs), "[guild-a guild-b guild-c]"; got != want {
		t.Fatalf("guild IDs = %s, want %s", got, want)
	}
}

func TestGuildPurgeUsesStrictCutoff(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cutoff := time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC)
	markers := map[string]time.Time{
		"before": cutoff.Add(-time.Millisecond),
		"at":     cutoff,
		"after":  cutoff.Add(time.Millisecond),
	}
	for guildID, removedAt := range markers {
		if err := s.MarkGuildRemoved(ctx, guildID, removedAt); err != nil {
			t.Fatal(err)
		}
	}

	guildIDs, err := s.GuildIDsRemovedBefore(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(guildIDs); got != "[before]" {
		t.Fatalf("eligible guilds = %s, want [before]", got)
	}
	purged, err := s.PurgeGuildRemovedBefore(ctx, "before", cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if !purged {
		t.Fatal("eligible guild was not purged")
	}

	rows, err := s.db.QueryContext(ctx, `SELECT guild_id FROM guild_removals ORDER BY guild_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var remaining []string
	for rows.Next() {
		var guildID string
		if err := rows.Scan(&guildID); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, guildID)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(remaining) != "[after at]" {
		t.Fatalf("remaining markers = %v, want [after at]", remaining)
	}
}

func TestPurgeGuildRemovedBeforeDeletesOnlyRemovedGuildData(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	removedAt := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)

	statements := []string{
		`INSERT INTO translation_groups(id,guild_id,display_name,created_by,created_at) VALUES
			('shared','guild-a','A','admin',1), ('shared','guild-b','B','admin',1)`,
		`INSERT INTO group_channels(group_id,guild_id,channel_id,channel_type,language,webhook_id,webhook_token) VALUES
			('shared','guild-a','a-source',0,'ja','a-wh-1','a-token-1'),
			('shared','guild-a','a-target',0,'en','a-wh-2','a-token-2'),
			('shared','guild-b','b-source',0,'ja','b-wh-1','b-token-1'),
			('shared','guild-b','b-target',0,'en','b-wh-2','b-token-2')`,
		`INSERT INTO thread_links(group_id,source_thread_id,source_channel_id,target_thread_id,target_channel_id,target_language) VALUES
			('shared','a-thread','a-source','a-target-thread','a-target','en'),
			('shared','b-thread','b-source','b-target-thread','b-target','en')`,
		`INSERT INTO message_links(source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_content_snapshot) VALUES
			(101,'a-source','shared','a-target','a-parent-copy','en','user','a parent'),
			(102,'a-thread','shared','a-target-thread','a-thread-copy','en','user','a thread'),
			(201,'b-source','shared','b-target','b-parent-copy','en','user','b parent'),
			(202,'b-thread','shared','b-target-thread','b-thread-copy','en','user','b thread')`,
		`INSERT INTO message_references(source_message_id,source_channel_id,referenced_message_id,referenced_channel_id) VALUES
			(101,'a-source',1,'a-source'), (102,'a-thread',2,'a-thread'),
			(201,'b-source',3,'b-source'), (202,'b-thread',4,'b-thread')`,
		`INSERT INTO pin_states(channel_id,message_id,pinned) VALUES
			('a-source',101,1), ('a-thread',102,1), ('b-source',201,1), ('b-thread',202,1)`,
		`INSERT INTO glossary_entries(guild_id,source_term,source_term_key,preferred_translation,created_by,created_at) VALUES
			('guild-a','a','a','A','admin',1), ('guild-b','b','b','B','admin',1)`,
		`INSERT INTO source_allowlists(guild_id,source_type,source_id,created_by,created_at) VALUES
			('guild-a','bot','101','admin',1), ('guild-b','bot','201','admin',1)`,
		`INSERT INTO poll_translation_cache(source_channel_id,source_message_id,language,answers_json,expires_at) VALUES
			('a-source',101,'en','["Red"]',999), ('a-thread',102,'en','["Yes"]',999),
			('b-source',201,'en','["Blue"]',999), ('b-thread',202,'en','["No"]',999)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkGuildRemoved(ctx, "guild-a", removedAt); err != nil {
		t.Fatal(err)
	}

	if purged, err := s.PurgeGuildRemovedBefore(ctx, "guild-a", removedAt); err != nil || purged {
		t.Fatalf("at cutoff purged=%v err=%v, want false", purged, err)
	}
	assertCount := func(query string, want int) {
		t.Helper()
		var got int
		if err := s.db.QueryRowContext(ctx, query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s: count=%d, want %d", query, got, want)
		}
	}
	for table, want := range map[string]int{
		"translation_groups": 2, "group_channels": 4, "thread_links": 2,
		"message_links": 4, "message_references": 4, "pin_states": 4,
		"glossary_entries": 2, "source_allowlists": 2, "guild_removals": 1,
		"poll_translation_cache": 4,
	} {
		assertCount(`SELECT COUNT(*) FROM `+table, want)
	}

	purged, err := s.PurgeGuildRemovedBefore(ctx, "guild-a", removedAt.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if !purged {
		t.Fatal("eligible guild was not purged")
	}

	assertCount(`SELECT COUNT(*) FROM translation_groups WHERE guild_id='guild-a'`, 0)
	assertCount(`SELECT COUNT(*) FROM group_channels WHERE guild_id='guild-a'`, 0)
	assertCount(`SELECT COUNT(*) FROM glossary_entries WHERE guild_id='guild-a'`, 0)
	assertCount(`SELECT COUNT(*) FROM source_allowlists WHERE guild_id='guild-a'`, 0)
	assertCount(`SELECT COUNT(*) FROM guild_removals WHERE guild_id='guild-a'`, 0)
	for _, table := range []string{"translation_groups", "thread_links", "glossary_entries", "source_allowlists"} {
		assertCount(`SELECT COUNT(*) FROM `+table, 1)
	}
	assertCount(`SELECT COUNT(*) FROM group_channels`, 2)
	for _, table := range []string{"message_links", "message_references", "pin_states"} {
		assertCount(`SELECT COUNT(*) FROM `+table, 2)
	}
	assertCount(`SELECT COUNT(*) FROM poll_translation_cache`, 2)
	assertCount(`SELECT COUNT(*) FROM poll_translation_cache WHERE source_channel_id IN ('a-source','a-thread')`, 0)
	assertCount(`SELECT COUNT(*) FROM message_links WHERE source_channel_id IN ('a-source','a-thread') OR target_channel_id IN ('a-target','a-target-thread')`, 0)
	assertCount(`SELECT COUNT(*) FROM message_links WHERE source_channel_id IN ('b-source','b-thread')`, 2)
}

func TestPurgeGuildRemovedBeforePreservesUnrelatedOrphanMessageReference(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	statements := []string{
		`INSERT INTO translation_groups(id,guild_id,display_name,created_by,created_at)
			VALUES('group','removed-guild','Removed','admin',1),
			('group','retained-guild','Retained','admin',1)`,
		`INSERT INTO group_channels(group_id,guild_id,channel_id,channel_type,language,webhook_id,webhook_token)
			VALUES('group','removed-guild','removed-channel',0,'ja','removed-webhook','removed-token'),
			('group','retained-guild','retained-channel',0,'en','retained-webhook','retained-token')`,
		`INSERT INTO message_references(source_message_id,source_channel_id,referenced_message_id,referenced_channel_id)
			VALUES(1,'retained-channel',2,'retained-channel')`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkGuildRemoved(ctx, "removed-guild", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}

	purged, err := s.PurgeGuildRemovedBefore(ctx, "removed-guild", time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !purged {
		t.Fatal("eligible guild was not purged")
	}
	var references int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_references
		WHERE source_channel_id='retained-channel' AND referenced_channel_id='retained-channel'`).Scan(&references); err != nil {
		t.Fatal(err)
	}
	if references != 1 {
		t.Fatalf("unrelated orphan message references = %d, want 1", references)
	}
}

func TestAllowedSourceCRUDDuplicateNotFoundAndGuildIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const sourceID = "18446744073709551615"

	if err := s.AddAllowedSource(ctx, "guild-a", SourceTypeBot, sourceID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAllowedSource(ctx, "guild-a", SourceTypeBot, sourceID, "admin"); !errors.Is(err, ErrSourceAlreadyAllowed) {
		t.Fatalf("duplicate error = %v", err)
	}
	for _, guildID := range []string{"guild-a", "guild-b"} {
		allowed, err := s.IsMessageSourceAllowed(ctx, guildID, SourceTypeBot, sourceID)
		if err != nil {
			t.Fatal(err)
		}
		if allowed != (guildID == "guild-a") {
			t.Fatalf("guild %s allowed = %v", guildID, allowed)
		}
	}
	sources, err := s.ListAllowedSources(ctx, "guild-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].GuildID != "guild-a" || sources[0].Type != SourceTypeBot || sources[0].ID != sourceID || sources[0].CreatedBy != "admin" {
		t.Fatalf("sources = %#v", sources)
	}
	var storedCreatedAt int64
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM source_allowlists WHERE guild_id=? AND source_id=?`, "guild-a", sourceID).Scan(&storedCreatedAt); err != nil {
		t.Fatal(err)
	}
	if storedCreatedAt < 1_000_000_000_000 {
		t.Fatalf("created_at = %d, want Unix milliseconds", storedCreatedAt)
	}
	if sources[0].CreatedAt.UnixMilli() != storedCreatedAt {
		t.Fatalf("CreatedAt = %v (%d), want %d", sources[0].CreatedAt, sources[0].CreatedAt.UnixMilli(), storedCreatedAt)
	}
	if err := s.RemoveAllowedSource(ctx, "guild-b", SourceTypeBot, sourceID); !errors.Is(err, ErrSourceNotAllowed) {
		t.Fatalf("cross-guild remove error = %v", err)
	}
	if err := s.RemoveAllowedSource(ctx, "guild-a", SourceTypeBot, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveAllowedSource(ctx, "guild-a", SourceTypeBot, sourceID); !errors.Is(err, ErrSourceNotAllowed) {
		t.Fatalf("missing remove error = %v", err)
	}
}

func TestAllowedSourceRejectsMalformedIDsAndTypes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"", "0", "01", "+1", "-1", "not-a-snowflake", "18446744073709551616"} {
		if err := s.AddAllowedSource(ctx, "guild", SourceTypeBot, id, "admin"); !errors.Is(err, ErrInvalidSnowflake) {
			t.Errorf("id %q error = %v", id, err)
		}
	}
	if err := s.AddAllowedSource(ctx, "guild", SourceType("user"), "123456789012345678", "admin"); !errors.Is(err, ErrInvalidSourceType) {
		t.Fatalf("source type error = %v", err)
	}
}

func TestAllowedSourceRejectsAndRemovesManagedWebhooks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const webhookID = "123456789012345678"
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "first", GuildID: "guild", DisplayName: "first", CreatedBy: "admin"}, GroupChannel{
		GroupID: "first", GuildID: "guild", ChannelID: "channel-1", Language: "ja", WebhookID: webhookID, WebhookToken: "token",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAllowedSource(ctx, "guild", SourceTypeWebhook, webhookID, "admin"); !errors.Is(err, ErrManagedWebhook) {
		t.Fatalf("managed webhook add error = %v", err)
	}

	const laterManagedID = "234567890123456789"
	if err := s.AddAllowedSource(ctx, "guild", SourceTypeWebhook, laterManagedID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "second", GuildID: "guild", DisplayName: "second", CreatedBy: "admin"}, GroupChannel{
		GroupID: "second", GuildID: "guild", ChannelID: "channel-2", Language: "en", WebhookID: laterManagedID, WebhookToken: "token",
	}); err != nil {
		t.Fatal(err)
	}
	sources, err := s.ListAllowedSources(ctx, "guild")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 0 {
		t.Fatalf("managed webhook remained allowlisted: %#v", sources)
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

func TestJoinChannelRejectsMismatchedChannelType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "c1", ChannelType: 0, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "forum-en", ChannelType: 15, Language: "en", WebhookID: "w2", WebhookToken: "t2",
	}); !errors.Is(err, ErrChannelTypeMismatch) {
		t.Fatalf("want ErrChannelTypeMismatch for forum into text group, got %v", err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "news-en", ChannelType: 5, Language: "en", WebhookID: "w3", WebhookToken: "t3",
	}); !errors.Is(err, ErrChannelTypeMismatch) {
		t.Fatalf("want ErrChannelTypeMismatch for news into text group, got %v", err)
	}

	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "forums", GuildID: "g1", DisplayName: "forums", CreatedBy: "u1"}, GroupChannel{
		GroupID: "forums", GuildID: "g1", ChannelID: "forum-ja", ChannelType: 15, Language: "ja", WebhookID: "w4", WebhookToken: "t4",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "forums", GuildID: "g1", ChannelID: "text-en", ChannelType: 0, Language: "en", WebhookID: "w5", WebhookToken: "t5",
	}); !errors.Is(err, ErrChannelTypeMismatch) {
		t.Fatalf("want ErrChannelTypeMismatch for text into forum group, got %v", err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "forums", GuildID: "g1", ChannelID: "forum-en", ChannelType: 15, Language: "en", WebhookID: "w6", WebhookToken: "t6",
	}); err != nil {
		t.Fatalf("same-type join should succeed: %v", err)
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

func TestForumTagMapUpsertLookupAndDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "forum-ja", ChannelType: 15, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "forum-en", ChannelType: 15, Language: "en", WebhookID: "w2", WebhookToken: "t2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertForumTagMap(ctx, "g1", "team", "forum-ja", "tag-ja-1", "forum-en", "tag-en-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertForumTagMap(ctx, "g1", "team", "forum-en", "tag-en-2", "forum-ja", "tag-ja-2"); err != nil {
		t.Fatal(err)
	}

	forward, err := s.ForumTagMapsBetween(ctx, "g1", "team", "forum-ja", "forum-en")
	if err != nil {
		t.Fatal(err)
	}
	if forward["tag-ja-1"] != "tag-en-1" || forward["tag-ja-2"] != "tag-en-2" {
		t.Fatalf("unexpected forward map: %#v", forward)
	}
	reverse, err := s.ForumTagMapsBetween(ctx, "g1", "team", "forum-en", "forum-ja")
	if err != nil {
		t.Fatal(err)
	}
	if reverse["tag-en-1"] != "tag-ja-1" || reverse["tag-en-2"] != "tag-ja-2" {
		t.Fatalf("unexpected reverse map: %#v", reverse)
	}

	if err := s.UpsertForumTagMap(ctx, "g1", "team", "forum-ja", "tag-ja-1", "forum-en", "tag-en-9"); err != nil {
		t.Fatal(err)
	}
	forward, err = s.ForumTagMapsBetween(ctx, "g1", "team", "forum-ja", "forum-en")
	if err != nil {
		t.Fatal(err)
	}
	if forward["tag-ja-1"] != "tag-en-9" {
		t.Fatalf("upsert did not replace: %#v", forward)
	}

	if err := s.DeleteForumTagMap(ctx, "g1", "team", "forum-ja", "tag-ja-1", "forum-en"); err != nil {
		t.Fatal(err)
	}
	forward, err = s.ForumTagMapsBetween(ctx, "g1", "team", "forum-ja", "forum-en")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := forward["tag-ja-1"]; ok {
		t.Fatalf("deleted mapping still present: %#v", forward)
	}
	if forward["tag-ja-2"] != "tag-en-2" {
		t.Fatalf("unrelated mapping removed: %#v", forward)
	}
}

func TestLeaveChannelRemovesForumTagMaps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "team", GuildID: "g1", DisplayName: "team", CreatedBy: "u1"}, GroupChannel{
		GroupID: "team", GuildID: "g1", ChannelID: "forum-ja", ChannelType: 15, Language: "ja", WebhookID: "w1", WebhookToken: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{GroupID: "team", GuildID: "g1", ChannelID: "forum-en", ChannelType: 15, Language: "en", WebhookID: "w2", WebhookToken: "t2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertForumTagMap(ctx, "g1", "team", "forum-ja", "tag-ja-1", "forum-en", "tag-en-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.LeaveChannel(ctx, "g1", "team", "forum-en"); err != nil {
		t.Fatal(err)
	}
	forward, err := s.ForumTagMapsBetween(ctx, "g1", "team", "forum-ja", "forum-en")
	if err != nil {
		t.Fatal(err)
	}
	if len(forward) != 0 {
		t.Fatalf("forum tag maps were not removed: %#v", forward)
	}
}

func TestMapAppliedForumTags(t *testing.T) {
	mapping := map[string]string{"a": "x", "b": "y"}
	got := MapAppliedForumTags(mapping, []string{"a", "missing", "b", "a"})
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("unexpected mapped tags: %#v", got)
	}
	if got := MapAppliedForumTags(nil, []string{"a"}); got != nil {
		t.Fatalf("want nil, got %#v", got)
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
	if err := s.SaveMessageLinkWithReference(ctx, MessageLink{
		SourceMessageID: "100000000000000002", SourceChannelID: "ja", GroupID: "team",
		TargetChannelID: "en", TargetMessageID: "target-reply", TargetLanguage: "en",
	}, MessageReference{MessageID: "100000000000000001", ChannelID: "ja"}); err != nil {
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
