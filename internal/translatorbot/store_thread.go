package translatorbot

import (
	"context"
	"database/sql"
	"errors"
)

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
