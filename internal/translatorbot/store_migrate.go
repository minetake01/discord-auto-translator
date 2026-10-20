package translatorbot

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// schemaMigration upgrades the SQLite schema by one version inside a single
// transaction. PRAGMA user_version records the last applied migration.
type schemaMigration func(ctx context.Context, tx *sql.Tx) error

// schemaMigrations is the ordered schema history. Index i upgrades
// user_version i to i+1. Append only; never edit an applied migration.
var schemaMigrations = []schemaMigration{
	migrateBaselineSchema,
	migrateBackfillSchema,
	migrateDropProcessedEvents,
	migrateResetPollCacheSeconds,
}

func currentSchemaVersion() int { return len(schemaMigrations) }

// migrate applies every pending migration. A database written by a newer
// binary fails fast instead of being opened with an unknown schema. File
// databases are copied with VACUUM INTO before the first pending migration.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	version, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}
	target := currentSchemaVersion()
	if version > target {
		return fmt.Errorf("SQLite schema version %d is newer than this binary supports (%d)", version, target)
	}
	if version == target {
		return nil
	}
	populated, err := s.hasTables(ctx)
	if err != nil {
		return err
	}
	if populated {
		if err := s.backupBeforeMigration(ctx, version); err != nil {
			return err
		}
	}
	for v := version; v < target; v++ {
		if err := s.applyMigration(ctx, v); err != nil {
			return fmt.Errorf("SQLite migration %d -> %d: %w", v, v+1, err)
		}
	}
	return nil
}

// hasTables reports whether the database already holds a schema; a fresh
// file has nothing to back up.
func (s *Store) hasTables(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *Store) applyMigration(ctx context.Context, from int) error {
	// Table rebuilds need foreign key enforcement off; the pragma is a no-op
	// inside a transaction, so it is toggled around it.
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() { _, _ = s.db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`) }()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := schemaMigrations[from](ctx, tx); err != nil {
		return err
	}
	if err := checkForeignKeys(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, from+1)); err != nil {
		return err
	}
	return tx.Commit()
}

func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("foreign key violation after migration")
	}
	return rows.Err()
}

// backupBeforeMigration writes a consistent copy next to the database file.
// In-memory databases have nothing to protect.
func (s *Store) backupBeforeMigration(ctx context.Context, version int) error {
	path := strings.TrimSpace(s.path)
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return nil
	}
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimPrefix(path, "file:")
	backup := fmt.Sprintf("%s.v%d-%s.bak", path, version, time.Now().UTC().Format("20060102T150405Z"))
	if _, err := os.Stat(backup); err == nil {
		return fmt.Errorf("migration backup %s already exists", backup)
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, backup); err != nil {
		return fmt.Errorf("back up SQLite database before migration: %w", err)
	}
	return nil
}

func execAll(ctx context.Context, tx *sql.Tx, stmts ...string) error {
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func columnTypes(ctx context.Context, tx *sql.Tx, table string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[string]string)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, declaredType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &declaredType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		found[name] = strings.ToUpper(declaredType)
	}
	return found, rows.Err()
}

// migrateBaselineSchema creates the schema that existed before versioned
// migrations. Databases created by earlier releases already contain these
// tables; IF NOT EXISTS and the column checks adopt them as version 1.
func migrateBaselineSchema(ctx context.Context, tx *sql.Tx) error {
	if err := execAll(ctx, tx,
		`CREATE TABLE IF NOT EXISTS translation_groups (
			id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			style_preset TEXT NOT NULL DEFAULT '',
			style_custom TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (guild_id, id)
		)`,
		`CREATE TABLE IF NOT EXISTS group_channels (
			group_id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			channel_type INTEGER NOT NULL,
			language TEXT NOT NULL,
			webhook_id TEXT NOT NULL,
			webhook_token TEXT NOT NULL,
			PRIMARY KEY (group_id, guild_id, channel_id),
			UNIQUE (group_id, guild_id, language),
			FOREIGN KEY (guild_id, group_id) REFERENCES translation_groups(guild_id, id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS message_links (
			source_message_id INTEGER NOT NULL,
			source_channel_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			target_channel_id TEXT NOT NULL,
			target_message_id TEXT NOT NULL,
			target_language TEXT NOT NULL,
			source_author_id TEXT NOT NULL,
			source_author_display_name TEXT NOT NULL DEFAULT '',
			source_content_snapshot TEXT NOT NULL,
			PRIMARY KEY (source_message_id, source_channel_id, target_channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS message_references (
			source_message_id INTEGER NOT NULL,
			source_channel_id TEXT NOT NULL,
			referenced_message_id INTEGER NOT NULL,
			referenced_channel_id TEXT NOT NULL,
			PRIMARY KEY (source_message_id, source_channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS thread_links (
			group_id TEXT NOT NULL,
			source_thread_id TEXT NOT NULL,
			source_channel_id TEXT NOT NULL DEFAULT '',
			target_thread_id TEXT NOT NULL,
			target_channel_id TEXT NOT NULL,
			target_language TEXT NOT NULL,
			PRIMARY KEY (group_id, source_thread_id, target_channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS pin_states (
			channel_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			pinned INTEGER NOT NULL,
			PRIMARY KEY (channel_id, message_id)
		)`,
		`CREATE TABLE IF NOT EXISTS processed_events (
			event_id TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS glossary_entries (
			guild_id TEXT NOT NULL,
			source_term TEXT NOT NULL,
			source_term_key TEXT NOT NULL,
			preferred_translation TEXT NOT NULL,
			attribute TEXT NOT NULL DEFAULT '',
			always_include INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (guild_id, source_term_key)
		)`,
		`CREATE TABLE IF NOT EXISTS source_allowlists (
			guild_id TEXT NOT NULL,
			source_type TEXT NOT NULL CHECK (source_type IN ('bot', 'webhook')),
			source_id TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (guild_id, source_type, source_id)
		)`,
		`CREATE TABLE IF NOT EXISTS guild_removals (
			guild_id TEXT PRIMARY KEY,
			removed_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS poll_translation_cache (
			source_channel_id TEXT NOT NULL,
			source_message_id INTEGER NOT NULL,
			language TEXT NOT NULL,
			answers_json TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			PRIMARY KEY (source_channel_id, source_message_id, language)
		)`,
		`CREATE TABLE IF NOT EXISTS forum_tag_maps (
			guild_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			channel_a_id TEXT NOT NULL,
			tag_a_id TEXT NOT NULL,
			channel_b_id TEXT NOT NULL,
			tag_b_id TEXT NOT NULL,
			PRIMARY KEY (guild_id, group_id, channel_a_id, tag_a_id, channel_b_id),
			UNIQUE (guild_id, group_id, channel_b_id, tag_b_id, channel_a_id),
			FOREIGN KEY (guild_id, group_id) REFERENCES translation_groups(guild_id, id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS topic_summaries (
			guild_id TEXT NOT NULL,
			location_key TEXT NOT NULL,
			generation_id TEXT NOT NULL,
			summary TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (location_key)
		)`,
	); err != nil {
		return err
	}
	links, err := columnTypes(ctx, tx, "message_links")
	if err != nil {
		return err
	}
	if _, ok := links["source_image_attachments"]; !ok {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE message_links ADD COLUMN source_image_attachments TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return err
		}
	}
	required := map[string]map[string]string{
		"translation_groups":     {"created_at": "INTEGER"},
		"message_links":          {"source_message_id": "INTEGER"},
		"pin_states":             {"message_id": "INTEGER"},
		"glossary_entries":       {"created_at": "INTEGER"},
		"poll_translation_cache": {"source_message_id": "INTEGER", "expires_at": "INTEGER"},
		"topic_summaries":        {"created_at": "INTEGER"},
	}
	for table, columns := range required {
		found, err := columnTypes(ctx, tx, table)
		if err != nil {
			return err
		}
		for column, want := range columns {
			if found[column] != want {
				return fmt.Errorf("unsupported pre-versioned schema: %s.%s is %q, want %s", table, column, found[column], want)
			}
		}
	}
	return execAll(ctx, tx,
		`CREATE INDEX IF NOT EXISTS idx_group_channels_guild_channel ON group_channels(guild_id, channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_forum_tag_maps_pair ON forum_tag_maps(guild_id, group_id, channel_a_id, channel_b_id)`,
		`CREATE INDEX IF NOT EXISTS idx_forum_tag_maps_channel ON forum_tag_maps(guild_id, group_id, channel_a_id)`,
		`CREATE INDEX IF NOT EXISTS idx_forum_tag_maps_channel_b ON forum_tag_maps(guild_id, group_id, channel_b_id)`,
		`CREATE INDEX IF NOT EXISTS idx_poll_translation_cache_expires_at ON poll_translation_cache(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_poll_translation_cache_source ON poll_translation_cache(source_channel_id, source_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_references_source ON message_references(source_channel_id, source_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_references_target ON message_references(referenced_channel_id, referenced_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_source_thread ON thread_links(source_thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_target_thread ON thread_links(target_thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_group_target_thread ON thread_links(group_id, target_thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_group_source_channel ON thread_links(group_id, source_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_group_target_channel ON thread_links(group_id, target_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_source_channel ON thread_links(source_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_target_channel ON thread_links(target_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pin_states_message ON pin_states(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_processed_events_created_at ON processed_events(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_guild_removals_removed_at ON guild_removals(removed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_topic_summaries_guild ON topic_summaries(guild_id)`,
		managedWebhookDenyTrigger("reject_managed_webhook_allowlist_insert", "INSERT"),
		managedWebhookDenyTrigger("reject_managed_webhook_allowlist_update", "UPDATE"),
		`CREATE TRIGGER IF NOT EXISTS remove_new_managed_webhook_allowlist
		AFTER INSERT ON group_channels
		BEGIN
			DELETE FROM source_allowlists
			WHERE guild_id = NEW.guild_id AND source_type = 'webhook' AND source_id = NEW.webhook_id;
		END`,
	)
}

// managedWebhookDenyTrigger builds the BEFORE INSERT/UPDATE trigger that
// rejects allowlisting one of the bot's own translation webhooks, raising
// the shared deny message for the error mapping to read back.
func managedWebhookDenyTrigger(name, event string) string {
	return fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s
	BEFORE %s ON source_allowlists
	WHEN NEW.source_type = 'webhook' AND EXISTS (
		SELECT 1 FROM group_channels
		WHERE guild_id = NEW.guild_id AND webhook_id = NEW.source_id
	)
	BEGIN
		SELECT RAISE(ABORT, '%s');
	END`, name, event, managedWebhookDenyMessage)
}

// migrateBackfillSchema rebuilds message_links with an INTEGER mirror
// message ID (its snowflake is the translation time used by retention) and a
// flag for the backfill timestamp line, and adds backfill job storage.
func migrateBackfillSchema(ctx context.Context, tx *sql.Tx) error {
	var nonNumeric int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_links WHERE target_message_id = '' OR target_message_id GLOB '*[^0-9]*'`).Scan(&nonNumeric); err != nil {
		return err
	}
	if nonNumeric > 0 {
		return fmt.Errorf("message_links has %d non-snowflake target_message_id values", nonNumeric)
	}
	return execAll(ctx, tx,
		`CREATE TABLE message_links_v2 (
			source_message_id INTEGER NOT NULL,
			source_channel_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			target_channel_id TEXT NOT NULL,
			target_message_id INTEGER NOT NULL,
			target_language TEXT NOT NULL,
			source_author_id TEXT NOT NULL,
			source_author_display_name TEXT NOT NULL DEFAULT '',
			source_content_snapshot TEXT NOT NULL,
			source_image_attachments TEXT NOT NULL DEFAULT '[]',
			timestamp_line INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (source_message_id, source_channel_id, target_channel_id)
		)`,
		`INSERT INTO message_links_v2(source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot,source_image_attachments)
			SELECT source_message_id,source_channel_id,group_id,target_channel_id,CAST(target_message_id AS INTEGER),target_language,source_author_id,source_author_display_name,source_content_snapshot,source_image_attachments FROM message_links`,
		`DROP TABLE message_links`,
		`ALTER TABLE message_links_v2 RENAME TO message_links`,
		`CREATE INDEX idx_message_links_source_channel_message ON message_links(source_channel_id, source_message_id)`,
		`CREATE INDEX idx_message_links_target_channel_message ON message_links(target_channel_id, target_message_id)`,
		`CREATE INDEX idx_message_links_group_source_channel ON message_links(group_id, source_channel_id)`,
		`CREATE INDEX idx_message_links_group_target_channel ON message_links(group_id, target_channel_id)`,
		`CREATE INDEX idx_message_links_target_message ON message_links(target_message_id)`,
		`CREATE TABLE backfill_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guild_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			target_channel_id TEXT NOT NULL,
			requested_by TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			UNIQUE (guild_id, group_id, target_channel_id),
			FOREIGN KEY (group_id, guild_id, target_channel_id) REFERENCES group_channels(group_id, guild_id, channel_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX idx_backfill_jobs_guild ON backfill_jobs(guild_id, id)`,
		`CREATE TABLE backfill_skipped_messages (
			job_id INTEGER NOT NULL,
			source_channel_id TEXT NOT NULL,
			source_message_id INTEGER NOT NULL,
			reason TEXT NOT NULL,
			PRIMARY KEY (job_id, source_channel_id, source_message_id),
			FOREIGN KEY (job_id) REFERENCES backfill_jobs(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE backfill_topic_summaries (
			job_id INTEGER NOT NULL,
			location_key TEXT NOT NULL,
			generation_id TEXT NOT NULL,
			summary TEXT NOT NULL,
			PRIMARY KEY (job_id, location_key),
			FOREIGN KEY (job_id) REFERENCES backfill_jobs(id) ON DELETE CASCADE
		)`,
	)
}

// migrateDropProcessedEvents removes the processed_events bookkeeping. The
// message_links row written with each post already records that a source
// message was mirrored into a target channel.
func migrateDropProcessedEvents(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx, `DROP TABLE IF EXISTS processed_events`)
}

// migrateResetPollCacheSeconds clears the poll translation cache, whose
// expires_at switched from Unix seconds to the store-wide Unix milliseconds.
// Cached translations are disposable, so the rows are cleared instead of
// being rescaled.
func migrateResetPollCacheSeconds(ctx context.Context, tx *sql.Tx) error {
	return execAll(ctx, tx, `DELETE FROM poll_translation_cache`)
}
