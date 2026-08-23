package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingType int
	err = tx.QueryRowContext(ctx, `SELECT channel_type FROM group_channels WHERE guild_id=? AND group_id=? LIMIT 1`, ch.GuildID, ch.GroupID).Scan(&existingType)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingType != ch.ChannelType {
		return ErrChannelTypeMismatch
	}
	if err := insertGroupChannel(ctx, tx, ch); err != nil {
		return err
	}
	return tx.Commit()
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM poll_translation_cache
		WHERE source_channel_id IN (SELECT channel_id FROM group_channels WHERE guild_id=? AND group_id=?)`, guildID, groupID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM topic_summaries WHERE guild_id=? AND (
		location_key=? OR location_key LIKE ?
	)`, guildID, guildID+":"+groupID+":group", guildID+":"+groupID+":thread:%"); err != nil {
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM poll_translation_cache WHERE source_channel_id=?`, channelID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM forum_tag_maps WHERE guild_id=? AND group_id=? AND (channel_a_id=? OR channel_b_id=?)`, guildID, groupID, channelID, channelID); err != nil {
		return err
	}
	return tx.Commit()
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
