package translatorbot

import (
	"context"
	"errors"
)

// normalizeForumTagMapEndpoints orders channel/tag pairs so channel_a_id < channel_b_id.
func normalizeForumTagMapEndpoints(channelAID, tagAID, channelBID, tagBID string) (string, string, string, string) {
	if channelAID < channelBID {
		return channelAID, tagAID, channelBID, tagBID
	}
	return channelBID, tagBID, channelAID, tagAID
}

// UpsertForumTagMap stores or replaces a bidirectional tag correspondence.
func (s *Store) UpsertForumTagMap(ctx context.Context, guildID, groupID, channelAID, tagAID, channelBID, tagBID string) error {
	if guildID == "" || groupID == "" || channelAID == "" || tagAID == "" || channelBID == "" || tagBID == "" {
		return errors.New("forum tag map fields must be non-empty")
	}
	if channelAID == channelBID {
		return errors.New("forum tag map channels must differ")
	}
	aCh, aTag, bCh, bTag := normalizeForumTagMapEndpoints(channelAID, tagAID, channelBID, tagBID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Clear prior mappings for either endpoint on this channel pair so PK/UNIQUE
	// stay 1:1 after channel-id normalization.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM forum_tag_maps
		WHERE guild_id=? AND group_id=? AND (
			(channel_a_id=? AND tag_a_id=? AND channel_b_id=?) OR
			(channel_b_id=? AND tag_b_id=? AND channel_a_id=?) OR
			(channel_a_id=? AND tag_a_id=? AND channel_b_id=?) OR
			(channel_b_id=? AND tag_b_id=? AND channel_a_id=?)
		)`,
		guildID, groupID,
		channelAID, tagAID, channelBID,
		channelAID, tagAID, channelBID,
		channelBID, tagBID, channelAID,
		channelBID, tagBID, channelAID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO forum_tag_maps (guild_id, group_id, channel_a_id, tag_a_id, channel_b_id, tag_b_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		guildID, groupID, aCh, aTag, bCh, bTag); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteForumTagMap removes the correspondence for one tag on channelAID toward channelBID.
func (s *Store) DeleteForumTagMap(ctx context.Context, guildID, groupID, channelAID, tagAID, channelBID string) error {
	if guildID == "" || groupID == "" || channelAID == "" || tagAID == "" || channelBID == "" {
		return errors.New("forum tag map fields must be non-empty")
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM forum_tag_maps
		WHERE guild_id=? AND group_id=? AND (
			(channel_a_id=? AND tag_a_id=? AND channel_b_id=?) OR
			(channel_b_id=? AND tag_b_id=? AND channel_a_id=?)
		)`,
		guildID, groupID, channelAID, tagAID, channelBID, channelAID, tagAID, channelBID)
	return err
}

// ForumTagMapsBetween returns sourceTagID → targetTagID for one directed channel pair.
func (s *Store) ForumTagMapsBetween(ctx context.Context, guildID, groupID, sourceChannelID, targetChannelID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT channel_a_id, tag_a_id, channel_b_id, tag_b_id
		FROM forum_tag_maps
		WHERE guild_id=? AND group_id=? AND (
			(channel_a_id=? AND channel_b_id=?) OR
			(channel_a_id=? AND channel_b_id=?)
		)`,
		guildID, groupID, sourceChannelID, targetChannelID, targetChannelID, sourceChannelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var aCh, aTag, bCh, bTag string
		if err := rows.Scan(&aCh, &aTag, &bCh, &bTag); err != nil {
			return nil, err
		}
		if aCh == sourceChannelID && bCh == targetChannelID {
			out[aTag] = bTag
		} else if bCh == sourceChannelID && aCh == targetChannelID {
			out[bTag] = aTag
		}
	}
	return out, rows.Err()
}

// ListForumTagMapsBetween returns ordered pairs as focusTag → peerTag for display.
func (s *Store) ListForumTagMapsBetween(ctx context.Context, guildID, groupID, focusChannelID, peerChannelID string) ([][2]string, error) {
	m, err := s.ForumTagMapsBetween(ctx, guildID, groupID, focusChannelID, peerChannelID)
	if err != nil {
		return nil, err
	}
	out := make([][2]string, 0, len(m))
	for focusTag, peerTag := range m {
		out = append(out, [2]string{focusTag, peerTag})
	}
	return out, nil
}

// MapAppliedForumTags maps source applied tag IDs onto the target channel using stored pairs.
// Unmapped source tags are omitted.
func MapAppliedForumTags(mapping map[string]string, sourceApplied []string) []string {
	if len(sourceApplied) == 0 || len(mapping) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(sourceApplied))
	out := make([]string, 0, len(sourceApplied))
	for _, src := range sourceApplied {
		dst, ok := mapping[src]
		if !ok || dst == "" {
			continue
		}
		if _, dup := seen[dst]; dup {
			continue
		}
		seen[dst] = struct{}{}
		out = append(out, dst)
	}
	return out
}
