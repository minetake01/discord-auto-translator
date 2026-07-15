package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SourceType string

const (
	SourceTypeBot     SourceType = "bot"
	SourceTypeWebhook SourceType = "webhook"
)

type AllowedSource struct {
	GuildID   string
	Type      SourceType
	ID        string
	CreatedBy string
	CreatedAt time.Time
}

func ParseSourceType(value string) (SourceType, error) {
	t := SourceType(strings.TrimSpace(value))
	if t != SourceTypeBot && t != SourceTypeWebhook {
		return "", ErrInvalidSourceType
	}
	return t, nil
}

func ValidateCanonicalSnowflake(id string) error {
	if id == "" || strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return ErrInvalidSnowflake
	}
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil || value == 0 || id != strconv.FormatUint(value, 10) {
		return ErrInvalidSnowflake
	}
	return nil
}

func (s *Store) AddAllowedSource(ctx context.Context, guildID string, sourceType SourceType, sourceID, createdBy string) error {
	if _, err := ParseSourceType(string(sourceType)); err != nil {
		return err
	}
	if err := ValidateCanonicalSnowflake(sourceID); err != nil {
		return err
	}
	if sourceType == SourceTypeWebhook {
		managed, err := s.IsManagedWebhook(ctx, guildID, sourceID)
		if err != nil {
			return err
		}
		if managed {
			return ErrManagedWebhook
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO source_allowlists(guild_id,source_type,source_id,created_by,created_at) VALUES(?,?,?,?,?)`,
		guildID, sourceType, sourceID, createdBy, time.Now().UTC().Unix())
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "translation output webhooks cannot be allowlisted"):
			return ErrManagedWebhook
		case strings.Contains(err.Error(), "UNIQUE constraint failed"):
			return ErrSourceAlreadyAllowed
		default:
			return err
		}
	}
	return nil
}

func (s *Store) RemoveAllowedSource(ctx context.Context, guildID string, sourceType SourceType, sourceID string) error {
	if _, err := ParseSourceType(string(sourceType)); err != nil {
		return err
	}
	if err := ValidateCanonicalSnowflake(sourceID); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM source_allowlists WHERE guild_id=? AND source_type=? AND source_id=?`, guildID, sourceType, sourceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrSourceNotAllowed
	}
	return nil
}

func (s *Store) ListAllowedSources(ctx context.Context, guildID string) ([]AllowedSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT guild_id,source_type,source_id,created_by,created_at FROM source_allowlists WHERE guild_id=? ORDER BY source_type,source_id`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []AllowedSource
	for rows.Next() {
		var source AllowedSource
		var createdAt int64
		if err := rows.Scan(&source.GuildID, &source.Type, &source.ID, &source.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		source.CreatedAt = time.Unix(createdAt, 0).UTC()
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (s *Store) IsManagedWebhook(ctx context.Context, guildID, webhookID string) (bool, error) {
	if err := ValidateCanonicalSnowflake(webhookID); err != nil {
		return false, err
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM group_channels WHERE guild_id=? AND webhook_id=?)`, guildID, webhookID).Scan(&exists)
	return exists, err
}

// IsMessageSourceAllowed is the fail-closed lookup used by the message policy.
// Managed translation webhooks are denied before the source allowlist is read.
func (s *Store) IsMessageSourceAllowed(ctx context.Context, guildID string, sourceType SourceType, sourceID string) (bool, error) {
	if _, err := ParseSourceType(string(sourceType)); err != nil {
		return false, err
	}
	if err := ValidateCanonicalSnowflake(sourceID); err != nil {
		return false, err
	}
	var allowed bool
	var err error
	if sourceType == SourceTypeWebhook {
		err = s.db.QueryRowContext(ctx, `SELECT
			NOT EXISTS(SELECT 1 FROM group_channels WHERE guild_id=? AND webhook_id=?)
			AND EXISTS(SELECT 1 FROM source_allowlists WHERE guild_id=? AND source_type='webhook' AND source_id=?)`,
			guildID, sourceID, guildID, sourceID).Scan(&allowed)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_allowlists WHERE guild_id=? AND source_type='bot' AND source_id=?)`, guildID, sourceID).Scan(&allowed)
	}
	if err != nil {
		return false, fmt.Errorf("lookup %s source %s: %w", sourceType, sourceID, err)
	}
	return allowed, nil
}

func isSourceValidationError(err error) bool {
	return errors.Is(err, ErrInvalidSourceType) || errors.Is(err, ErrInvalidSnowflake)
}
