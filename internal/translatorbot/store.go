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
	path               string
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
	s := &Store{db: db, path: path}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

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
