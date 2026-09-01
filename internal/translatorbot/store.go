package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

var (
	ErrDuplicateGroup       = errors.New("translation group already exists in this guild")
	ErrDuplicateChannel     = errors.New("channel already exists in this group")
	ErrDuplicateLanguage    = errors.New("language already exists in this group")
	ErrChannelTypeMismatch  = errors.New("channel type does not match existing channels in this group")
	ErrGroupNotFound        = errors.New("translation group not found in this guild")
	ErrChannelNotFound      = errors.New("channel is not joined to this group")
	ErrGlossaryFull         = errors.New("glossary is full for this server")
	ErrGlossaryNotFound     = errors.New("glossary entry not found")
	ErrGlossaryTermRequired = errors.New("glossary term and translation are required")
	ErrSourceAlreadyAllowed = errors.New("message source is already allowed in this guild")
	ErrSourceNotAllowed     = errors.New("message source is not allowed in this guild")
	ErrInvalidSourceType    = errors.New("message source type must be bot or webhook")
	ErrInvalidSnowflake     = errors.New("source ID must be a canonical nonzero uint64 snowflake")
	ErrManagedWebhook       = errors.New("translation output webhooks cannot be allowlisted")
)

type Store struct {
	db                 *sql.DB
	saveMessageLinkErr error // set only in tests to simulate persistence failure
}

func OpenStore(path string) (*Store, error) {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	db, err := sql.Open("sqlite", path+separator+"_pragma=busy_timeout%3d2000&_pragma=foreign_keys%3dON")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Init(ctx context.Context) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON`,
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
			source_image_attachments TEXT NOT NULL DEFAULT '[]',
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
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_group_channels_guild_channel ON group_channels(guild_id, channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_forum_tag_maps_pair ON forum_tag_maps(guild_id, group_id, channel_a_id, channel_b_id)`,
		`CREATE INDEX IF NOT EXISTS idx_forum_tag_maps_channel ON forum_tag_maps(guild_id, group_id, channel_a_id)`,
		`CREATE INDEX IF NOT EXISTS idx_forum_tag_maps_channel_b ON forum_tag_maps(guild_id, group_id, channel_b_id)`,
		`CREATE INDEX IF NOT EXISTS idx_poll_translation_cache_expires_at ON poll_translation_cache(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_poll_translation_cache_source ON poll_translation_cache(source_channel_id, source_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_source_channel_message ON message_links(source_channel_id, source_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_target_channel_message ON message_links(target_channel_id, target_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_group_source_channel ON message_links(group_id, source_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_group_target_channel ON message_links(group_id, target_channel_id)`,
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
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "message_links", "source_image_attachments", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.validateOptimizedSchema(ctx); err != nil {
		return err
	}
	for _, stmt := range indexes {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS reject_managed_webhook_allowlist_insert
		BEFORE INSERT ON source_allowlists
		WHEN NEW.source_type = 'webhook' AND EXISTS (
			SELECT 1 FROM group_channels
			WHERE guild_id = NEW.guild_id AND webhook_id = NEW.source_id
		)
		BEGIN
			SELECT RAISE(ABORT, 'translation output webhooks cannot be allowlisted');
		END`,
		`CREATE TRIGGER IF NOT EXISTS reject_managed_webhook_allowlist_update
		BEFORE UPDATE ON source_allowlists
		WHEN NEW.source_type = 'webhook' AND EXISTS (
			SELECT 1 FROM group_channels
			WHERE guild_id = NEW.guild_id AND webhook_id = NEW.source_id
		)
		BEGIN
			SELECT RAISE(ABORT, 'translation output webhooks cannot be allowlisted');
		END`,
		`CREATE TRIGGER IF NOT EXISTS remove_new_managed_webhook_allowlist
		AFTER INSERT ON group_channels
		BEGIN
			DELETE FROM source_allowlists
			WHERE guild_id = NEW.guild_id AND source_type = 'webhook' AND source_id = NEW.webhook_id;
		END`,
	}
	for _, stmt := range triggers {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) tableColumnTypes(ctx context.Context, table string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return found, nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, declaration string) error {
	types, err := s.tableColumnTypes(ctx, table)
	if err != nil {
		return err
	}
	if _, ok := types[column]; ok {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+declaration)
	return err
}

func (s *Store) validateOptimizedSchema(ctx context.Context) error {
	required := map[string]map[string]string{
		"translation_groups":     {"created_at": "INTEGER"},
		"message_links":          {"source_message_id": "INTEGER", "source_image_attachments": "TEXT"},
		"pin_states":             {"message_id": "INTEGER"},
		"processed_events":       {"created_at": "INTEGER"},
		"glossary_entries":       {"created_at": "INTEGER"},
		"poll_translation_cache": {"source_message_id": "INTEGER", "expires_at": "INTEGER"},
		"topic_summaries":        {"created_at": "INTEGER"},
	}
	for table, columns := range required {
		found, err := s.tableColumnTypes(ctx, table)
		if err != nil {
			return err
		}
		for column, want := range columns {
			if found[column] != want {
				return fmt.Errorf("incompatible SQLite schema: %s.%s must be %s (run the one-time migration)", table, column, want)
			}
		}
	}
	return nil
}

func parseDiscordSnowflakeID(field, id string) (int64, error) {
	value, err := strconv.ParseInt(id, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive decimal int64: %q", field, id)
	}
	return value, nil
}

func normalizeLanguage(s string) string { return strings.TrimSpace(s) }

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func deleteOrphanedMessageReferences(ctx context.Context, x execer) error {
	_, err := x.ExecContext(ctx, `DELETE FROM message_references
		WHERE NOT EXISTS (
			SELECT 1 FROM message_links ml
			WHERE ml.source_channel_id=message_references.source_channel_id
			AND ml.source_message_id=message_references.source_message_id
		)`)
	return err
}
