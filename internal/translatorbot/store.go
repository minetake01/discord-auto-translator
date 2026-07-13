package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const glossaryMaxEntries = 50

var (
	ErrDuplicateGroup       = errors.New("translation group already exists in this guild")
	ErrDuplicateChannel     = errors.New("channel already exists in this group")
	ErrDuplicateLanguage    = errors.New("language already exists in this group")
	ErrGroupNotFound        = errors.New("translation group not found in this guild")
	ErrChannelNotFound      = errors.New("channel is not joined to this group")
	ErrGlossaryFull         = errors.New("glossary is full for this server")
	ErrGlossaryNotFound     = errors.New("glossary entry not found")
	ErrGlossaryTermRequired = errors.New("glossary term and translation are required")
)

type Store struct {
	db                 *sql.DB
	saveMessageLinkErr error // set only in tests to simulate persistence failure
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
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
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_group_channels_guild_channel ON group_channels(guild_id, channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_source_channel_message ON message_links(source_channel_id, source_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_target_channel_message ON message_links(target_channel_id, target_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_group_source_channel ON message_links(group_id, source_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_links_group_target_channel ON message_links(group_id, target_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_message_references_target ON message_references(referenced_channel_id, referenced_message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_source_thread ON thread_links(source_thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_target_thread ON thread_links(target_thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_group_target_thread ON thread_links(group_id, target_thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_group_source_channel ON thread_links(group_id, source_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_thread_links_group_target_channel ON thread_links(group_id, target_channel_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pin_states_message ON pin_states(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_processed_events_created_at ON processed_events(created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.validateOptimizedSchema(ctx); err != nil {
		return err
	}
	for _, stmt := range indexes {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateOptimizedSchema(ctx context.Context) error {
	required := map[string]map[string]string{
		"translation_groups": {"created_at": "INTEGER"},
		"message_links":      {"source_message_id": "INTEGER"},
		"pin_states":         {"message_id": "INTEGER"},
		"processed_events":   {"created_at": "INTEGER"},
		"glossary_entries":   {"created_at": "INTEGER"},
	}
	for table, columns := range required {
		rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			return err
		}
		found := make(map[string]string, len(columns))
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, declaredType string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &declaredType, &notNull, &defaultValue, &primaryKey); err != nil {
				_ = rows.Close()
				return err
			}
			if _, ok := columns[name]; ok {
				found[name] = strings.ToUpper(declaredType)
			}
		}
		if err := rows.Close(); err != nil {
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

func (s *Store) CreateGroupWithChannel(ctx context.Context, g TranslationGroup, ch GroupChannel) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO translation_groups(id,guild_id,display_name,created_by,created_at) VALUES(?,?,?,?,?)`,
		g.ID, g.GuildID, g.DisplayName, g.CreatedBy, g.CreatedAt.UnixMilli())
	if err != nil {
		if strings.Contains(err.Error(), "constraint") {
			return ErrDuplicateGroup
		}
		return err
	}
	if err := insertGroupChannel(ctx, tx, ch); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) JoinChannel(ctx context.Context, ch GroupChannel) error {
	return insertGroupChannel(ctx, s.db, ch)
}

func (s *Store) DeleteGroup(ctx context.Context, guildID, groupID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_links
		WHERE group_id=?
		AND (
			source_channel_id IN (SELECT channel_id FROM group_channels WHERE guild_id=? AND group_id=?)
			OR target_channel_id IN (SELECT channel_id FROM group_channels WHERE guild_id=? AND group_id=?)
		)`, groupID, guildID, groupID, guildID, groupID); err != nil {
		return err
	}
	if err := deleteOrphanedMessageReferences(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_links
		WHERE group_id=?
		AND (
			source_channel_id IN (SELECT channel_id FROM group_channels WHERE guild_id=? AND group_id=?)
			OR target_channel_id IN (SELECT channel_id FROM group_channels WHERE guild_id=? AND group_id=?)
		)`, groupID, guildID, groupID, guildID, groupID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM translation_groups WHERE guild_id=? AND id=?`, guildID, groupID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrGroupNotFound
	}
	return tx.Commit()
}

func (s *Store) LeaveChannel(ctx context.Context, guildID, groupID, channelID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM group_channels WHERE guild_id=? AND group_id=? AND channel_id=?`, guildID, groupID, channelID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrChannelNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_links WHERE group_id=? AND (source_channel_id=? OR target_channel_id=?)`, groupID, channelID, channelID); err != nil {
		return err
	}
	if err := deleteOrphanedMessageReferences(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_links WHERE group_id=? AND (source_channel_id=? OR target_channel_id=?)`, groupID, channelID, channelID); err != nil {
		return err
	}
	return tx.Commit()
}

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

func insertGroupChannel(ctx context.Context, x execer, ch GroupChannel) error {
	_, err := x.ExecContext(ctx, `INSERT INTO group_channels(group_id,guild_id,channel_id,channel_type,language,webhook_id,webhook_token) VALUES(?,?,?,?,?,?,?)`,
		ch.GroupID, ch.GuildID, ch.ChannelID, ch.ChannelType, normalizeLanguage(ch.Language), ch.WebhookID, ch.WebhookToken)
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return ErrGroupNotFound
		}
		if strings.Contains(err.Error(), "group_channels.group_id") || strings.Contains(err.Error(), "channel_id") {
			return ErrDuplicateChannel
		}
		if strings.Contains(err.Error(), "language") {
			return ErrDuplicateLanguage
		}
	}
	return err
}

func (s *Store) GroupExists(ctx context.Context, guildID, groupID string) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT 1 FROM translation_groups WHERE guild_id=? AND id=? LIMIT 1`, guildID, groupID)
	var one int
	err := row.Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) Groups(ctx context.Context, guildID, query string, limit int) ([]TranslationGroup, error) {
	q := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT id,guild_id,display_name,created_by,created_at,style_preset,style_custom FROM translation_groups
		WHERE guild_id=? AND (lower(id) LIKE ? OR lower(display_name) LIKE ?)
		ORDER BY display_name LIMIT ?`, guildID, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TranslationGroup
	for rows.Next() {
		var g TranslationGroup
		var ts int64
		if err := rows.Scan(&g.ID, &g.GuildID, &g.DisplayName, &g.CreatedBy, &ts, &g.StylePreset, &g.StyleCustom); err != nil {
			return nil, err
		}
		g.CreatedAt = time.UnixMilli(ts).UTC()
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) ChannelsByChannel(ctx context.Context, guildID, channelID string) ([]GroupChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id,guild_id,channel_id,channel_type,language,webhook_id,webhook_token FROM group_channels WHERE guild_id=? AND channel_id=?`, guildID, channelID)
	if err != nil {
		return nil, err
	}
	return scanChannels(rows)
}

func (s *Store) ChannelsInGroup(ctx context.Context, guildID, groupID string) ([]GroupChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id,guild_id,channel_id,channel_type,language,webhook_id,webhook_token FROM group_channels WHERE guild_id=? AND group_id=?`, guildID, groupID)
	if err != nil {
		return nil, err
	}
	return scanChannels(rows)
}

func scanChannels(rows *sql.Rows) ([]GroupChannel, error) {
	defer rows.Close()
	var out []GroupChannel
	for rows.Next() {
		var c GroupChannel
		if err := rows.Scan(&c.GroupID, &c.GuildID, &c.ChannelID, &c.ChannelType, &c.Language, &c.WebhookID, &c.WebhookToken); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetPinState(ctx context.Context, channelID, messageID string) (pinned bool, known bool, err error) {
	messageIDValue, err := parseDiscordSnowflakeID("message_id", messageID)
	if err != nil {
		return false, false, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT pinned FROM pin_states WHERE channel_id=? AND message_id=?`, channelID, messageIDValue)
	var value int
	err = row.Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return value != 0, true, nil
}

func (s *Store) SavePinState(ctx context.Context, channelID, messageID string, pinned bool) error {
	messageIDValue, err := parseDiscordSnowflakeID("message_id", messageID)
	if err != nil {
		return err
	}
	value := 0
	if pinned {
		value = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO pin_states(channel_id,message_id,pinned) VALUES(?,?,?)`, channelID, messageIDValue, value)
	return err
}

func (s *Store) UpdateMessageLinkSnapshot(ctx context.Context, sourceChannelID, sourceMessageID, targetChannelID, snapshot string) error {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE message_links SET source_content_snapshot=? WHERE source_channel_id=? AND source_message_id=? AND target_channel_id=?`,
		snapshot, sourceChannelID, sourceMessageIDValue, targetChannelID)
	return err
}

func (s *Store) SaveMessageLink(ctx context.Context, l MessageLink) error {
	if s.saveMessageLinkErr != nil {
		return s.saveMessageLinkErr
	}
	sourceMessageID, err := parseDiscordSnowflakeID("source_message_id", l.SourceMessageID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO message_links(source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot) VALUES(?,?,?,?,?,?,?,?,?)`,
		sourceMessageID, l.SourceChannelID, l.GroupID, l.TargetChannelID, l.TargetMessageID, l.TargetLanguage, l.SourceAuthorID, l.SourceAuthorDisplayName, l.SourceContentSnapshot); err != nil {
		return err
	}
	if l.ReferencedMessageID != "" {
		if l.ReferencedChannelID == "" {
			return errors.New("referenced_channel_id is required when referenced_message_id is set")
		}
		referencedMessageID, err := parseDiscordSnowflakeID("referenced_message_id", l.ReferencedMessageID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO message_references(source_message_id,source_channel_id,referenced_message_id,referenced_channel_id) VALUES(?,?,?,?)`,
			sourceMessageID, l.SourceChannelID, referencedMessageID, l.ReferencedChannelID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteMessageData(ctx context.Context, sourceChannelID, sourceMessageID string, copies []MessageLink) error {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_links WHERE source_channel_id=? AND source_message_id=?`, sourceChannelID, sourceMessageIDValue); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_references
		WHERE (source_channel_id=? AND source_message_id=?) OR (referenced_channel_id=? AND referenced_message_id=?)`,
		sourceChannelID, sourceMessageIDValue, sourceChannelID, sourceMessageIDValue); err != nil {
		return err
	}
	for _, copy := range copies {
		copyMessageID, err := parseDiscordSnowflakeID("target_message_id", copy.TargetMessageID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_references WHERE referenced_channel_id=? AND referenced_message_id=?`, copy.TargetChannelID, copyMessageID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteMessageLinksByChannel(ctx context.Context, channelID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_links WHERE source_channel_id=? OR target_channel_id=?`, channelID, channelID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_references WHERE source_channel_id=? OR referenced_channel_id=?`, channelID, channelID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MessageTargets(ctx context.Context, sourceChannelID, sourceMessageID string) ([]MessageLink, error) {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot FROM message_links WHERE source_channel_id=? AND source_message_id=?`, sourceChannelID, sourceMessageIDValue)
	if err != nil {
		return nil, err
	}
	return scanMessageLinks(rows)
}

func (s *Store) MessageTargetsReplyingTo(ctx context.Context, referencedChannelID, referencedMessageID string) ([]MessageLink, error) {
	referencedMessageIDValue, err := parseDiscordSnowflakeID("referenced_message_id", referencedMessageID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ml.source_message_id,ml.source_channel_id,ml.group_id,ml.target_channel_id,ml.target_message_id,ml.target_language,ml.source_author_id,ml.source_author_display_name,ml.source_content_snapshot
		FROM message_links ml
		JOIN message_references mr ON mr.source_channel_id=ml.source_channel_id AND mr.source_message_id=ml.source_message_id
		WHERE mr.referenced_channel_id=? AND mr.referenced_message_id=?`, referencedChannelID, referencedMessageIDValue)
	if err != nil {
		return nil, err
	}
	return scanMessageLinks(rows)
}

func (s *Store) MessagePeers(ctx context.Context, channelID, messageID string) ([]MessageLink, error) {
	peers, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot FROM message_links WHERE target_channel_id=? AND target_message_id=?`, channelID, messageID)
	if err != nil {
		return nil, err
	}
	reverse, err := scanMessageLinks(rows)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, peer := range peers {
		seen[peer.TargetChannelID+"\x00"+peer.TargetMessageID] = true
	}
	for _, link := range reverse {
		key := link.SourceChannelID + "\x00" + link.SourceMessageID
		if !seen[key] {
			peers = append(peers, MessageLink{
				SourceMessageID: link.SourceMessageID, SourceChannelID: link.SourceChannelID, GroupID: link.GroupID,
				TargetChannelID: link.SourceChannelID, TargetMessageID: link.SourceMessageID, TargetLanguage: "",
				SourceAuthorID: link.SourceAuthorID, SourceAuthorDisplayName: link.SourceAuthorDisplayName, SourceContentSnapshot: link.SourceContentSnapshot,
			})
			seen[key] = true
		}
		targets, err := s.MessageTargets(ctx, link.SourceChannelID, link.SourceMessageID)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			key := target.TargetChannelID + "\x00" + target.TargetMessageID
			if target.TargetChannelID == channelID && target.TargetMessageID == messageID {
				continue
			}
			if !seen[key] {
				peers = append(peers, target)
				seen[key] = true
			}
		}
	}
	return peers, nil
}

type MessageOriginalResult struct {
	SourceChannelID         string
	SourceMessageID         string
	SourceAuthorDisplayName string
	Snapshot                string
	TargetLanguage          string
	IsSource                bool
}

func (s *Store) MessageOriginal(ctx context.Context, channelID, messageID string) (MessageOriginalResult, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot FROM message_links WHERE target_channel_id=? AND target_message_id=? LIMIT 1`, channelID, messageID)
	var link MessageLink
	err := row.Scan(&link.SourceMessageID, &link.SourceChannelID, &link.GroupID, &link.TargetChannelID, &link.TargetMessageID, &link.TargetLanguage, &link.SourceAuthorID, &link.SourceAuthorDisplayName, &link.SourceContentSnapshot)
	if err == nil {
		return MessageOriginalResult{
			SourceChannelID:         link.SourceChannelID,
			SourceMessageID:         link.SourceMessageID,
			SourceAuthorDisplayName: link.SourceAuthorDisplayName,
			Snapshot:                link.SourceContentSnapshot,
			TargetLanguage:          link.TargetLanguage,
			IsSource:                false,
		}, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MessageOriginalResult{}, false, err
	}

	links, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return MessageOriginalResult{}, false, err
	}
	if len(links) == 0 {
		return MessageOriginalResult{}, false, nil
	}
	link = links[0]
	return MessageOriginalResult{
		SourceChannelID:         channelID,
		SourceMessageID:         messageID,
		SourceAuthorDisplayName: link.SourceAuthorDisplayName,
		Snapshot:                link.SourceContentSnapshot,
		IsSource:                true,
	}, true, nil
}

func (s *Store) MessageQuoteTarget(ctx context.Context, channelID, messageID, targetChannelID string) (content, quoteChannelID, quoteMessageID string, ok bool, err error) {
	links, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return "", "", "", false, err
	}
	if len(links) > 0 {
		link := links[0]
		for _, target := range links {
			if target.TargetChannelID == targetChannelID {
				return link.SourceContentSnapshot, target.TargetChannelID, target.TargetMessageID, true, nil
			}
		}
		if link.SourceChannelID == targetChannelID {
			return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
		}
		return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
	}

	row := s.db.QueryRowContext(ctx, `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot FROM message_links WHERE target_channel_id=? AND target_message_id=? LIMIT 1`, channelID, messageID)
	var link MessageLink
	err = row.Scan(&link.SourceMessageID, &link.SourceChannelID, &link.GroupID, &link.TargetChannelID, &link.TargetMessageID, &link.TargetLanguage, &link.SourceAuthorID, &link.SourceAuthorDisplayName, &link.SourceContentSnapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	if link.TargetChannelID == targetChannelID {
		return link.SourceContentSnapshot, link.TargetChannelID, link.TargetMessageID, true, nil
	}
	if link.SourceChannelID == targetChannelID {
		return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
	}
	targets, err := s.MessageTargets(ctx, link.SourceChannelID, link.SourceMessageID)
	if err != nil {
		return "", "", "", false, err
	}
	for _, target := range targets {
		if target.TargetChannelID == targetChannelID {
			return link.SourceContentSnapshot, target.TargetChannelID, target.TargetMessageID, true, nil
		}
	}
	return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
}

func (s *Store) RecentMessageHistory(ctx context.Context, channelIDs []string, excludeMessageID string, limit int) ([]MessageLink, error) {
	if limit <= 0 || len(channelIDs) == 0 {
		return nil, nil
	}
	excludeMessageIDValue, err := parseDiscordSnowflakeID("exclude_message_id", excludeMessageID)
	if err != nil {
		return nil, err
	}
	placeholders := strings.Repeat("?,", len(channelIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(channelIDs)+2)
	for _, channelID := range channelIDs {
		args = append(args, channelID)
	}
	args = append(args, excludeMessageIDValue, limit)
	query := `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot
		FROM message_links
		WHERE source_channel_id IN (` + placeholders + `) AND source_message_id<>?
		GROUP BY source_channel_id, source_message_id
		ORDER BY source_message_id DESC
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	links, err := scanMessageLinks(rows)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(links)-1; i < j; i, j = i+1, j-1 {
		links[i], links[j] = links[j], links[i]
	}
	return links, nil
}

func scanMessageLinks(rows *sql.Rows) ([]MessageLink, error) {
	defer rows.Close()
	var out []MessageLink
	for rows.Next() {
		var l MessageLink
		if err := rows.Scan(&l.SourceMessageID, &l.SourceChannelID, &l.GroupID, &l.TargetChannelID, &l.TargetMessageID, &l.TargetLanguage, &l.SourceAuthorID, &l.SourceAuthorDisplayName, &l.SourceContentSnapshot); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) SaveThreadLink(ctx context.Context, l ThreadLink) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO thread_links(group_id,source_thread_id,source_channel_id,target_thread_id,target_channel_id,target_language) VALUES(?,?,?,?,?,?)`,
		l.GroupID, l.SourceThreadID, l.SourceChannelID, l.TargetThreadID, l.TargetChannelID, l.TargetLanguage)
	return err
}

func (s *Store) ThreadTargets(ctx context.Context, threadID string) ([]ThreadLink, error) {
	peers, err := s.SourceThreadTargets(ctx, threadID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT group_id,source_thread_id,source_channel_id,target_thread_id,target_channel_id,target_language FROM thread_links WHERE target_thread_id=?`, threadID)
	if err != nil {
		return nil, err
	}
	reverse, err := scanThreadLinks(rows)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, peer := range peers {
		seen[peer.TargetThreadID] = true
	}
	for _, link := range reverse {
		if !seen[link.SourceThreadID] {
			peers = append(peers, ThreadLink{
				GroupID: link.GroupID, SourceThreadID: link.TargetThreadID, SourceChannelID: link.TargetChannelID,
				TargetThreadID: link.SourceThreadID, TargetChannelID: link.SourceChannelID,
			})
			seen[link.SourceThreadID] = true
		}
		targets, err := s.ThreadTargets(ctx, link.SourceThreadID)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if target.TargetThreadID == threadID {
				continue
			}
			if !seen[target.TargetThreadID] {
				peers = append(peers, target)
				seen[target.TargetThreadID] = true
			}
		}
	}
	return peers, nil
}

func (s *Store) SourceThreadTargets(ctx context.Context, threadID string) ([]ThreadLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id,source_thread_id,source_channel_id,target_thread_id,target_channel_id,target_language FROM thread_links WHERE source_thread_id=?`, threadID)
	if err != nil {
		return nil, err
	}
	return scanThreadLinks(rows)
}

func (s *Store) ThreadParentChannel(ctx context.Context, groupID, threadID string) (string, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT target_channel_id FROM thread_links WHERE group_id=? AND target_thread_id=? LIMIT 1`, groupID, threadID)
	var channelID string
	err := row.Scan(&channelID)
	if err == nil {
		return channelID, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	row = s.db.QueryRowContext(ctx, `SELECT source_channel_id FROM thread_links WHERE group_id=? AND source_thread_id=? LIMIT 1`, groupID, threadID)
	err = row.Scan(&channelID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return channelID, true, nil
}

func (s *Store) DeleteThreadLinks(ctx context.Context, threadID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM thread_links WHERE source_thread_id=? OR target_thread_id=?`, threadID, threadID)
	return err
}

func (s *Store) PurgeMessageLinksOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	maxID := snowflakeIDBefore(cutoff.UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM message_links WHERE source_message_id < ?`, maxID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if err := deleteOrphanedMessageReferences(ctx, tx); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pin_states WHERE message_id < ?`, maxID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func scanThreadLinks(rows *sql.Rows) ([]ThreadLink, error) {
	defer rows.Close()
	var out []ThreadLink
	for rows.Next() {
		var l ThreadLink
		if err := rows.Scan(&l.GroupID, &l.SourceThreadID, &l.SourceChannelID, &l.TargetThreadID, &l.TargetChannelID, &l.TargetLanguage); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) MarkProcessed(ctx context.Context, id string) (bool, error) {
	now := time.Now().UTC()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM processed_events WHERE created_at < ?`, now.Add(-10*time.Minute).UnixMilli())
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO processed_events(event_id,created_at) VALUES(?,?)`, id, now.UnixMilli())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) IsEventProcessed(ctx context.Context, id string) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT 1 FROM processed_events WHERE event_id=? LIMIT 1`, id)
	var one int
	err := row.Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func glossaryTermKey(term string) string {
	return strings.ToLower(strings.TrimSpace(term))
}

func (s *Store) UpsertGlossaryEntry(ctx context.Context, guildID, term, translation, attribute, createdBy string, alwaysInclude bool) error {
	term = strings.TrimSpace(term)
	translation = strings.TrimSpace(translation)
	attribute = strings.TrimSpace(attribute)
	if term == "" || translation == "" {
		return ErrGlossaryTermRequired
	}
	key := glossaryTermKey(term)
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM glossary_entries WHERE guild_id=?`, guildID).Scan(&count); err != nil {
		return err
	}
	var existing int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM glossary_entries WHERE guild_id=? AND source_term_key=?`, guildID, key).Scan(&existing)
	if err != nil {
		return err
	}
	if existing == 0 && count >= glossaryMaxEntries {
		return ErrGlossaryFull
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO glossary_entries(guild_id,source_term,source_term_key,preferred_translation,attribute,always_include,created_by,created_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(guild_id, source_term_key) DO UPDATE SET
			source_term=excluded.source_term,
			preferred_translation=excluded.preferred_translation,
			attribute=excluded.attribute,
			always_include=excluded.always_include,
			created_by=excluded.created_by,
			created_at=excluded.created_at`,
		guildID, term, key, translation, attribute, alwaysInclude, createdBy, time.Now().UTC().UnixMilli())
	return err
}

func (s *Store) RemoveGlossaryEntry(ctx context.Context, guildID, term string) error {
	key := glossaryTermKey(term)
	if key == "" {
		return ErrGlossaryTermRequired
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM glossary_entries WHERE guild_id=? AND source_term_key=?`, guildID, key)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrGlossaryNotFound
	}
	return nil
}

func (s *Store) ListGlossaryEntries(ctx context.Context, guildID string) ([]GlossaryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_term, preferred_translation, attribute, always_include FROM glossary_entries WHERE guild_id=? ORDER BY source_term COLLATE NOCASE`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GlossaryEntry
	for rows.Next() {
		var entry GlossaryEntry
		if err := rows.Scan(&entry.SourceTerm, &entry.PreferredTranslation, &entry.Attribute, &entry.AlwaysInclude); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *Store) SetGroupStyle(ctx context.Context, guildID, groupID, preset, custom string) error {
	exists, err := s.GroupExists(ctx, guildID, groupID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrGroupNotFound
	}
	_, err = s.db.ExecContext(ctx, `UPDATE translation_groups SET style_preset=?, style_custom=? WHERE guild_id=? AND id=?`,
		strings.TrimSpace(preset), strings.TrimSpace(custom), guildID, groupID)
	return err
}

func (s *Store) GroupStyle(ctx context.Context, guildID, groupID string) (preset, custom string, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT style_preset, style_custom FROM translation_groups WHERE guild_id=? AND id=?`, guildID, groupID)
	err = row.Scan(&preset, &custom)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrGroupNotFound
	}
	return preset, custom, err
}

func normalizeLanguage(s string) string { return strings.TrimSpace(s) }
