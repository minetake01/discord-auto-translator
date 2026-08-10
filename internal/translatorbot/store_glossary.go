package translatorbot

import (
	"context"
	"strings"
	"time"
)

const glossaryMaxEntries = 50

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
