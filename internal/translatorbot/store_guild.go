package translatorbot

import (
	"context"
	"time"
)

func (s *Store) MarkGuildRemoved(ctx context.Context, guildID string, removedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO guild_removals(guild_id,removed_at) VALUES(?,?)
		ON CONFLICT(guild_id) DO UPDATE SET removed_at=excluded.removed_at`, guildID, removedAt.UnixMilli())
	return err
}

func (s *Store) CancelGuildRemoval(ctx context.Context, guildID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM guild_removals WHERE guild_id=?`, guildID)
	return err
}

func (s *Store) GuildIDsWithStoredData(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT guild_id FROM (
		SELECT guild_id FROM translation_groups
		UNION SELECT guild_id FROM group_channels
		UNION SELECT guild_id FROM glossary_entries
		UNION SELECT guild_id FROM source_allowlists
		UNION SELECT guild_id FROM guild_removals
	) WHERE guild_id <> '' ORDER BY guild_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var guildIDs []string
	for rows.Next() {
		var guildID string
		if err := rows.Scan(&guildID); err != nil {
			return nil, err
		}
		guildIDs = append(guildIDs, guildID)
	}
	return guildIDs, rows.Err()
}

func (s *Store) GuildIDsRemovedBefore(ctx context.Context, cutoff time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT guild_id FROM guild_removals WHERE removed_at < ? ORDER BY guild_id`, cutoff.UnixMilli())
	if err != nil {
		return nil, err
	}
	var guildIDs []string
	for rows.Next() {
		var guildID string
		if err := rows.Scan(&guildID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		guildIDs = append(guildIDs, guildID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return guildIDs, nil
}

func (s *Store) PurgeGuildRemovedBefore(ctx context.Context, guildID string, cutoff time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var eligible bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM guild_removals WHERE guild_id=? AND removed_at < ?
	)`, guildID, cutoff.UnixMilli()).Scan(&eligible); err != nil {
		return false, err
	}
	if !eligible {
		return false, tx.Commit()
	}
	guildChannelsAndThreads := `WITH guild_channels(channel_id) AS (
		SELECT channel_id FROM group_channels WHERE guild_id=?
	), guild_threads(thread_id) AS (
		SELECT source_thread_id FROM thread_links
		WHERE source_channel_id IN (SELECT channel_id FROM guild_channels)
			OR target_channel_id IN (SELECT channel_id FROM guild_channels)
		UNION
		SELECT target_thread_id FROM thread_links
		WHERE source_channel_id IN (SELECT channel_id FROM guild_channels)
			OR target_channel_id IN (SELECT channel_id FROM guild_channels)
	) `
	if _, err := tx.ExecContext(ctx, guildChannelsAndThreads+`DELETE FROM message_references
		WHERE source_channel_id IN (
			SELECT channel_id FROM guild_channels UNION SELECT thread_id FROM guild_threads
		) OR referenced_channel_id IN (
			SELECT channel_id FROM guild_channels UNION SELECT thread_id FROM guild_threads
		)`, guildID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, guildChannelsAndThreads+`DELETE FROM message_links
		WHERE source_channel_id IN (
			SELECT channel_id FROM guild_channels UNION SELECT thread_id FROM guild_threads
		) OR target_channel_id IN (
			SELECT channel_id FROM guild_channels UNION SELECT thread_id FROM guild_threads
		)`, guildID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, guildChannelsAndThreads+`DELETE FROM poll_translation_cache
		WHERE source_channel_id IN (
			SELECT channel_id FROM guild_channels UNION SELECT thread_id FROM guild_threads
		)`, guildID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, guildChannelsAndThreads+`DELETE FROM pin_states
		WHERE channel_id IN (
			SELECT channel_id FROM guild_channels UNION SELECT thread_id FROM guild_threads
		)`, guildID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_links WHERE
		source_channel_id IN (SELECT channel_id FROM group_channels WHERE guild_id=?)
		OR target_channel_id IN (SELECT channel_id FROM group_channels WHERE guild_id=?)`, guildID, guildID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM glossary_entries WHERE guild_id=?`, guildID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_allowlists WHERE guild_id=?`, guildID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM translation_groups WHERE guild_id=?`, guildID); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM guild_removals WHERE guild_id=? AND removed_at < ?`, guildID, cutoff.UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
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
