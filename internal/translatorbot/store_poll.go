package translatorbot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SavePollTranslationCache stores translated poll answers for one language until
// the poll expires (or the poll-result handler deletes the row).
func (s *Store) SavePollTranslationCache(ctx context.Context, sourceChannelID, sourceMessageID, language string, answers []string, expiresAt time.Time) error {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return err
	}
	if expiresAt.IsZero() {
		return fmt.Errorf("poll translation cache requires a non-zero expires_at")
	}
	payload, err := json.Marshal(answers)
	if err != nil {
		return fmt.Errorf("marshal poll answers: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO poll_translation_cache(source_channel_id,source_message_id,language,answers_json,expires_at) VALUES(?,?,?,?,?)`,
		sourceChannelID, sourceMessageIDValue, normalizeLanguage(language), string(payload), expiresAt.UTC().Unix())
	return err
}

// PollTranslatedAnswers returns cached translated answers for a poll message.
// ok is false when no row exists.
func (s *Store) PollTranslatedAnswers(ctx context.Context, sourceChannelID, sourceMessageID, language string) (answers []string, ok bool, err error) {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return nil, false, err
	}
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT answers_json FROM poll_translation_cache WHERE source_channel_id=? AND source_message_id=? AND language=?`,
		sourceChannelID, sourceMessageIDValue, normalizeLanguage(language)).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if err := json.Unmarshal([]byte(raw), &answers); err != nil {
		return nil, false, fmt.Errorf("unmarshal poll answers: %w", err)
	}
	return answers, true, nil
}

// DeletePollTranslationCache removes all language rows for one source poll.
func (s *Store) DeletePollTranslationCache(ctx context.Context, sourceChannelID, sourceMessageID string) error {
	if sourceMessageID == "" {
		return nil
	}
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM poll_translation_cache WHERE source_channel_id=? AND source_message_id=?`,
		sourceChannelID, sourceMessageIDValue)
	return err
}

// PurgeExpiredPollTranslationCache deletes orphan cache rows past expires_at.
func (s *Store) PurgeExpiredPollTranslationCache(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM poll_translation_cache WHERE expires_at < ?`, now.UTC().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
