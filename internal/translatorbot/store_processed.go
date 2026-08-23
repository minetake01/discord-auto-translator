package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

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
