package translatorbot

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// legacySchema is the unversioned schema that production databases had
// before migrations (user_version 0, TEXT target_message_id).
const legacySchema = `
CREATE TABLE translation_groups (id TEXT NOT NULL, guild_id TEXT NOT NULL, display_name TEXT NOT NULL, created_by TEXT NOT NULL, created_at INTEGER NOT NULL, style_preset TEXT NOT NULL DEFAULT '', style_custom TEXT NOT NULL DEFAULT '', PRIMARY KEY (guild_id, id));
CREATE TABLE group_channels (group_id TEXT NOT NULL, guild_id TEXT NOT NULL, channel_id TEXT NOT NULL, channel_type INTEGER NOT NULL, language TEXT NOT NULL, webhook_id TEXT NOT NULL, webhook_token TEXT NOT NULL, PRIMARY KEY (group_id, guild_id, channel_id), UNIQUE (group_id, guild_id, language), FOREIGN KEY (guild_id, group_id) REFERENCES translation_groups(guild_id, id) ON DELETE CASCADE);
CREATE TABLE message_links (source_message_id INTEGER NOT NULL, source_channel_id TEXT NOT NULL, group_id TEXT NOT NULL, target_channel_id TEXT NOT NULL, target_message_id TEXT NOT NULL, target_language TEXT NOT NULL, source_author_id TEXT NOT NULL, source_author_display_name TEXT NOT NULL DEFAULT '', source_content_snapshot TEXT NOT NULL, source_image_attachments TEXT NOT NULL DEFAULT '[]', PRIMARY KEY (source_message_id, source_channel_id, target_channel_id));
CREATE INDEX idx_message_links_target_channel_message ON message_links(target_channel_id, target_message_id);
CREATE TABLE pin_states (channel_id TEXT NOT NULL, message_id INTEGER NOT NULL, pinned INTEGER NOT NULL, PRIMARY KEY (channel_id, message_id));
CREATE TABLE processed_events (event_id TEXT PRIMARY KEY, created_at INTEGER NOT NULL);
CREATE TABLE glossary_entries (guild_id TEXT NOT NULL, source_term TEXT NOT NULL, preferred_translation TEXT NOT NULL, attribute TEXT NOT NULL DEFAULT '', always_include INTEGER NOT NULL DEFAULT 0, created_by TEXT NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY (guild_id, source_term));
CREATE TABLE poll_translation_cache (source_channel_id TEXT NOT NULL, source_message_id INTEGER NOT NULL, target_language TEXT NOT NULL, answers_json TEXT NOT NULL, expires_at INTEGER NOT NULL, PRIMARY KEY (source_channel_id, source_message_id, target_language));
CREATE TABLE topic_summaries (location_key TEXT NOT NULL, guild_id TEXT NOT NULL, generation_id TEXT NOT NULL, summary TEXT NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY (location_key));
INSERT INTO translation_groups VALUES ('g','guild','g','u',1,'','');
INSERT INTO group_channels VALUES ('g','guild','ja',0,'ja','w-ja','t-ja');
INSERT INTO group_channels VALUES ('g','guild','en',0,'en','w-en','t-en');
INSERT INTO message_links(source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_content_snapshot)
	VALUES (100000000000000001,'ja','g','en','100000000000000002','en','u','こんにちは');
`

func createLegacyDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range strings.Split(legacySchema, ";\n") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func TestOpenStoreMigratesLegacyDatabaseKeepingDataAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.db")
	createLegacyDB(t, path)

	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	version, err := s.schemaVersion(ctx)
	if err != nil || version != currentSchemaVersion() {
		t.Fatalf("version = %d err=%v, want %d", version, err, currentSchemaVersion())
	}
	links, err := s.MessageTargets(ctx, "ja", "100000000000000001")
	if err != nil || len(links) != 1 || links[0].TargetMessageID != "100000000000000002" || links[0].SourceContentSnapshot != "こんにちは" {
		t.Fatalf("migrated link = %#v err=%v", links, err)
	}
	// Mirror lookups must keep working with the INTEGER column.
	if original, ok, err := s.MessageOriginal(ctx, "en", "100000000000000002"); err != nil || !ok || original.SourceMessageID != "100000000000000001" {
		t.Fatalf("original = %#v ok=%v err=%v", original, ok, err)
	}
	backups, err := filepath.Glob(path + ".v0-*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v err=%v", backups, err)
	}
	backup, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var targetType string
	if err := backup.QueryRow(`SELECT typeof(target_message_id) FROM message_links`).Scan(&targetType); err != nil || targetType != "text" {
		t.Fatalf("backup should hold the pre-migration data: type=%q err=%v", targetType, err)
	}
	if _, err := s.JoinChannelWithBackfill(ctx, GroupChannel{GroupID: "g", GuildID: "guild", ChannelID: "fr", Language: "fr", WebhookID: "w", WebhookToken: "t"}, "u"); err != nil {
		t.Fatalf("backfill tables should exist after migration: %v", err)
	}
}

func TestOpenStoreReopensCurrentDatabaseWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if backups, _ := filepath.Glob(path + ".*.bak"); len(backups) != 0 {
		t.Fatalf("no migration ran, so no backup is expected: %v", backups)
	}
}

func TestOpenStoreRejectsDatabaseFromNewerBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := OpenStore(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("want newer-version error, got %v", err)
	}
}

func TestLegacyDatabaseWithNonSnowflakeMirrorIDFailsWithoutChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	createLegacyDB(t, path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE message_links SET target_message_id='broken'`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := OpenStore(path); err == nil {
		t.Fatal("migration should fail fast on a non-snowflake mirror id")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var value string
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT target_message_id FROM message_links`).Scan(&value); err != nil || value != "broken" {
		t.Fatalf("failed migration must leave the data untouched: %q err=%v", value, err)
	}
	if version != 1 {
		t.Fatalf("version = %d; the baseline step commits on its own and the failing step rolls back", version)
	}
}
