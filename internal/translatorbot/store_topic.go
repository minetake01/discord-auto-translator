package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) TopicSummary(ctx context.Context, locationKey, generationID string) (string, error) {
	locationKey = strings.TrimSpace(locationKey)
	generationID = strings.TrimSpace(generationID)
	if locationKey == "" || generationID == "" {
		return "", sql.ErrNoRows
	}
	var summary string
	err := s.db.QueryRowContext(ctx, `SELECT summary FROM topic_summaries WHERE location_key=? AND generation_id=?`, locationKey, generationID).Scan(&summary)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(summary), nil
}

func (s *Store) TopicSummaryForLocation(ctx context.Context, locationKey string) (generationID, summary string, err error) {
	locationKey = strings.TrimSpace(locationKey)
	if locationKey == "" {
		return "", "", sql.ErrNoRows
	}
	err = s.db.QueryRowContext(ctx, `SELECT generation_id, summary FROM topic_summaries WHERE location_key=?`, locationKey).Scan(&generationID, &summary)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(generationID), strings.TrimSpace(summary), nil
}

func (s *Store) UpsertTopicSummary(ctx context.Context, guildID, locationKey, generationID, summary string) error {
	guildID = strings.TrimSpace(guildID)
	locationKey = strings.TrimSpace(locationKey)
	generationID = strings.TrimSpace(generationID)
	summary = strings.TrimSpace(summary)
	if guildID == "" || locationKey == "" || generationID == "" || summary == "" {
		return errors.New("topic summary location, generation, and text are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO topic_summaries(guild_id,location_key,generation_id,summary,created_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(location_key) DO UPDATE SET
			guild_id=excluded.guild_id,
			generation_id=excluded.generation_id,
			summary=excluded.summary,
			created_at=excluded.created_at`,
		guildID, locationKey, generationID, summary, time.Now().UTC().UnixMilli())
	return err
}
