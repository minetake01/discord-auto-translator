package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrDuplicateGroup    = errors.New("translation group already exists in this guild")
	ErrDuplicateChannel  = errors.New("channel already exists in this group")
	ErrDuplicateLanguage = errors.New("language already exists in this group")
)

type Store struct {
	db *sql.DB
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
			created_at TEXT NOT NULL,
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
			source_message_id TEXT NOT NULL,
			source_channel_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			target_channel_id TEXT NOT NULL,
			target_message_id TEXT NOT NULL,
			target_language TEXT NOT NULL,
			source_author_id TEXT NOT NULL,
			source_content_snapshot TEXT NOT NULL,
			PRIMARY KEY (source_message_id, source_channel_id, target_channel_id)
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
			message_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			pinned INTEGER NOT NULL,
			PRIMARY KEY (message_id, channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS processed_events (
			event_id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE thread_links ADD COLUMN source_channel_id TEXT NOT NULL DEFAULT ''`)
	return nil
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
		g.ID, g.GuildID, g.DisplayName, g.CreatedBy, g.CreatedAt.Format(time.RFC3339Nano))
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

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertGroupChannel(ctx context.Context, x execer, ch GroupChannel) error {
	_, err := x.ExecContext(ctx, `INSERT INTO group_channels(group_id,guild_id,channel_id,channel_type,language,webhook_id,webhook_token) VALUES(?,?,?,?,?,?,?)`,
		ch.GroupID, ch.GuildID, ch.ChannelID, ch.ChannelType, normalizeLanguage(ch.Language), ch.WebhookID, ch.WebhookToken)
	if err != nil {
		if strings.Contains(err.Error(), "group_channels.group_id") || strings.Contains(err.Error(), "channel_id") {
			return ErrDuplicateChannel
		}
		if strings.Contains(err.Error(), "language") {
			return ErrDuplicateLanguage
		}
		if strings.Contains(err.Error(), "constraint") {
			return fmt.Errorf("duplicate channel or language: %w", err)
		}
	}
	return err
}

func (s *Store) Groups(ctx context.Context, guildID, query string, limit int) ([]TranslationGroup, error) {
	q := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT id,guild_id,display_name,created_by,created_at FROM translation_groups
		WHERE guild_id=? AND (lower(id) LIKE ? OR lower(display_name) LIKE ?)
		ORDER BY display_name LIMIT ?`, guildID, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TranslationGroup
	for rows.Next() {
		var g TranslationGroup
		var ts string
		if err := rows.Scan(&g.ID, &g.GuildID, &g.DisplayName, &g.CreatedBy, &ts); err != nil {
			return nil, err
		}
		g.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
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

func (s *Store) SaveMessageLink(ctx context.Context, l MessageLink) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO message_links(source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_content_snapshot) VALUES(?,?,?,?,?,?,?,?)`,
		l.SourceMessageID, l.SourceChannelID, l.GroupID, l.TargetChannelID, l.TargetMessageID, l.TargetLanguage, l.SourceAuthorID, l.SourceContentSnapshot)
	return err
}

func (s *Store) MessageTargets(ctx context.Context, sourceChannelID, sourceMessageID string) ([]MessageLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_content_snapshot FROM message_links WHERE source_channel_id=? AND source_message_id=?`, sourceChannelID, sourceMessageID)
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
	rows, err := s.db.QueryContext(ctx, `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_content_snapshot FROM message_links WHERE target_channel_id=? AND target_message_id=?`, channelID, messageID)
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
				SourceAuthorID: link.SourceAuthorID, SourceContentSnapshot: link.SourceContentSnapshot,
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

func (s *Store) MessageSnapshot(ctx context.Context, channelID, messageID string) (authorID, content string, ok bool, err error) {
	links, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return "", "", false, err
	}
	if len(links) > 0 {
		return links[0].SourceAuthorID, links[0].SourceContentSnapshot, true, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT source_author_id,source_content_snapshot FROM message_links WHERE target_channel_id=? AND target_message_id=? LIMIT 1`, channelID, messageID)
	err = row.Scan(&authorID, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return authorID, content, true, nil
}

func (s *Store) MessageQuoteTarget(ctx context.Context, channelID, messageID, targetChannelID string) (authorID, content, quoteChannelID, quoteMessageID string, ok bool, err error) {
	links, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return "", "", "", "", false, err
	}
	if len(links) > 0 {
		link := links[0]
		for _, target := range links {
			if target.TargetChannelID == targetChannelID {
				return link.SourceAuthorID, link.SourceContentSnapshot, target.TargetChannelID, target.TargetMessageID, true, nil
			}
		}
		if link.SourceChannelID == targetChannelID {
			return link.SourceAuthorID, link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
		}
		return link.SourceAuthorID, link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
	}

	row := s.db.QueryRowContext(ctx, `SELECT source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_content_snapshot FROM message_links WHERE target_channel_id=? AND target_message_id=? LIMIT 1`, channelID, messageID)
	var link MessageLink
	err = row.Scan(&link.SourceMessageID, &link.SourceChannelID, &link.GroupID, &link.TargetChannelID, &link.TargetMessageID, &link.TargetLanguage, &link.SourceAuthorID, &link.SourceContentSnapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", "", false, err
	}
	if link.TargetChannelID == targetChannelID {
		return link.SourceAuthorID, link.SourceContentSnapshot, link.TargetChannelID, link.TargetMessageID, true, nil
	}
	if link.SourceChannelID == targetChannelID {
		return link.SourceAuthorID, link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
	}
	targets, err := s.MessageTargets(ctx, link.SourceChannelID, link.SourceMessageID)
	if err != nil {
		return "", "", "", "", false, err
	}
	for _, target := range targets {
		if target.TargetChannelID == targetChannelID {
			return link.SourceAuthorID, link.SourceContentSnapshot, target.TargetChannelID, target.TargetMessageID, true, nil
		}
	}
	return link.SourceAuthorID, link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
}

func scanMessageLinks(rows *sql.Rows) ([]MessageLink, error) {
	defer rows.Close()
	var out []MessageLink
	for rows.Next() {
		var l MessageLink
		if err := rows.Scan(&l.SourceMessageID, &l.SourceChannelID, &l.GroupID, &l.TargetChannelID, &l.TargetMessageID, &l.TargetLanguage, &l.SourceAuthorID, &l.SourceContentSnapshot); err != nil {
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
	_, _ = s.db.ExecContext(ctx, `DELETE FROM processed_events WHERE created_at < ?`, time.Now().Add(-10*time.Minute).UTC().Format(time.RFC3339Nano))
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO processed_events(event_id,created_at) VALUES(?,?)`, id, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func normalizeLanguage(s string) string { return strings.TrimSpace(s) }
