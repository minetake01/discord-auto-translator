package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const messageLinkColumns = `source_message_id,source_channel_id,group_id,target_channel_id,target_message_id,target_language,source_author_id,source_author_display_name,source_content_snapshot,source_image_attachments,timestamp_line`

func (s *Store) UpdateMessageLinkSnapshot(ctx context.Context, sourceChannelID, sourceMessageID, targetChannelID, snapshot string) error {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE message_links SET source_content_snapshot=? WHERE source_channel_id=? AND source_message_id=? AND target_channel_id=?`,
		snapshot, sourceChannelID, sourceMessageIDValue, targetChannelID)
	return err
}

func (s *Store) SaveMessageLink(ctx context.Context, l MessageLink) error {
	return s.saveMessageLink(ctx, l, MessageReference{})
}

func (s *Store) SaveMessageLinkWithReference(ctx context.Context, l MessageLink, ref MessageReference) error {
	return s.saveMessageLink(ctx, l, ref)
}

func (s *Store) saveMessageLink(ctx context.Context, l MessageLink, ref MessageReference) error {
	if s.saveMessageLinkErr != nil {
		return s.saveMessageLinkErr
	}
	sourceMessageID, err := parseDiscordSnowflakeID("source_message_id", l.SourceMessageID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	imagesJSON, err := marshalImageAttachments(l.SourceImageAttachments)
	if err != nil {
		return err
	}
	targetMessageID, err := parseDiscordSnowflakeID("target_message_id", l.TargetMessageID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO message_links(`+messageLinkColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		sourceMessageID, l.SourceChannelID, l.GroupID, l.TargetChannelID, targetMessageID, l.TargetLanguage, l.SourceAuthorID, l.SourceAuthorDisplayName, l.SourceContentSnapshot, imagesJSON, l.TimestampLine); err != nil {
		return err
	}
	if ref.MessageID != "" {
		if ref.ChannelID == "" {
			return errors.New("referenced_channel_id is required when referenced_message_id is set")
		}
		referencedMessageID, err := parseDiscordSnowflakeID("referenced_message_id", ref.MessageID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO message_references(source_message_id,source_channel_id,referenced_message_id,referenced_channel_id) VALUES(?,?,?,?)`,
			sourceMessageID, l.SourceChannelID, referencedMessageID, ref.ChannelID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MessageReferenceOf returns the stored reply target of a source message.
func (s *Store) MessageReferenceOf(ctx context.Context, sourceChannelID, sourceMessageID string) (MessageReference, bool, error) {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return MessageReference{}, false, err
	}
	var ref MessageReference
	err = s.db.QueryRowContext(ctx, `SELECT CAST(referenced_message_id AS TEXT), referenced_channel_id FROM message_references WHERE source_channel_id=? AND source_message_id=?`,
		sourceChannelID, sourceMessageIDValue).Scan(&ref.MessageID, &ref.ChannelID)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageReference{}, false, nil
	}
	if err != nil {
		return MessageReference{}, false, err
	}
	return ref, true, nil
}

func (s *Store) DeleteMessageData(ctx context.Context, sourceChannelID, sourceMessageID string, copies []MessageLink) error {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_links WHERE source_channel_id=? AND source_message_id=?`, sourceChannelID, sourceMessageIDValue); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_references
		WHERE (source_channel_id=? AND source_message_id=?) OR (referenced_channel_id=? AND referenced_message_id=?)`,
		sourceChannelID, sourceMessageIDValue, sourceChannelID, sourceMessageIDValue); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM poll_translation_cache WHERE source_channel_id=? AND source_message_id=?`,
		sourceChannelID, sourceMessageIDValue); err != nil {
		return err
	}
	for _, copy := range copies {
		copyMessageID, err := parseDiscordSnowflakeID("target_message_id", copy.TargetMessageID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_references WHERE referenced_channel_id=? AND referenced_message_id=?`, copy.TargetChannelID, copyMessageID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteMessageLinksByChannel(ctx context.Context, channelID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_links WHERE source_channel_id=? OR target_channel_id=?`, channelID, channelID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_references WHERE source_channel_id=? OR referenced_channel_id=?`, channelID, channelID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MessageTargets(ctx context.Context, sourceChannelID, sourceMessageID string) ([]MessageLink, error) {
	sourceMessageIDValue, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageLinkColumns+` FROM message_links WHERE source_channel_id=? AND source_message_id=?`, sourceChannelID, sourceMessageIDValue)
	if err != nil {
		return nil, err
	}
	return scanMessageLinks(rows)
}

func (s *Store) MessageTargetsReplyingTo(ctx context.Context, referencedChannelID, referencedMessageID string) ([]MessageLink, error) {
	referencedMessageIDValue, err := parseDiscordSnowflakeID("referenced_message_id", referencedMessageID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ml.source_message_id,ml.source_channel_id,ml.group_id,ml.target_channel_id,ml.target_message_id,ml.target_language,ml.source_author_id,ml.source_author_display_name,ml.source_content_snapshot,ml.source_image_attachments,ml.timestamp_line
		FROM message_links ml
		JOIN message_references mr ON mr.source_channel_id=ml.source_channel_id AND mr.source_message_id=ml.source_message_id
		WHERE mr.referenced_channel_id=? AND mr.referenced_message_id=?`, referencedChannelID, referencedMessageIDValue)
	if err != nil {
		return nil, err
	}
	return scanMessageLinks(rows)
}

func (s *Store) MessagePeers(ctx context.Context, channelID, messageID string) ([]MessageLink, error) {
	peers, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageLinkColumns+` FROM message_links WHERE target_channel_id=? AND target_message_id=?`, channelID, messageID)
	if err != nil {
		return nil, err
	}
	reverse, err := scanMessageLinks(rows)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, peer := range peers {
		seen[peer.TargetChannelID+"\x00"+peer.TargetMessageID] = true
	}
	for _, link := range reverse {
		key := link.SourceChannelID + "\x00" + link.SourceMessageID
		if !seen[key] {
			peers = append(peers, MessageLink{
				SourceMessageID: link.SourceMessageID, SourceChannelID: link.SourceChannelID, GroupID: link.GroupID,
				TargetChannelID: link.SourceChannelID, TargetMessageID: link.SourceMessageID, TargetLanguage: "",
				SourceAuthorID: link.SourceAuthorID, SourceAuthorDisplayName: link.SourceAuthorDisplayName, SourceContentSnapshot: link.SourceContentSnapshot,
			})
			seen[key] = true
		}
		targets, err := s.MessageTargets(ctx, link.SourceChannelID, link.SourceMessageID)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			key := target.TargetChannelID + "\x00" + target.TargetMessageID
			if target.TargetChannelID == channelID && target.TargetMessageID == messageID {
				continue
			}
			if !seen[key] {
				peers = append(peers, target)
				seen[key] = true
			}
		}
	}
	return peers, nil
}

type MessageOriginalResult struct {
	SourceChannelID         string
	SourceMessageID         string
	SourceAuthorDisplayName string
	Snapshot                string
	ImageAttachments        []DiscordAttachment
	TargetLanguage          string
	IsSource                bool
}

func (s *Store) MessageOriginal(ctx context.Context, channelID, messageID string) (MessageOriginalResult, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+messageLinkColumns+` FROM message_links WHERE target_channel_id=? AND target_message_id=? LIMIT 1`, channelID, messageID)
	link, err := scanMessageLink(row)
	if err == nil {
		return MessageOriginalResult{
			SourceChannelID:         link.SourceChannelID,
			SourceMessageID:         link.SourceMessageID,
			SourceAuthorDisplayName: link.SourceAuthorDisplayName,
			Snapshot:                link.SourceContentSnapshot,
			ImageAttachments:        link.SourceImageAttachments,
			TargetLanguage:          link.TargetLanguage,
			IsSource:                false,
		}, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MessageOriginalResult{}, false, err
	}

	links, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return MessageOriginalResult{}, false, err
	}
	if len(links) == 0 {
		return MessageOriginalResult{}, false, nil
	}
	link = links[0]
	return MessageOriginalResult{
		SourceChannelID:         channelID,
		SourceMessageID:         messageID,
		SourceAuthorDisplayName: link.SourceAuthorDisplayName,
		Snapshot:                link.SourceContentSnapshot,
		ImageAttachments:        link.SourceImageAttachments,
		IsSource:                true,
	}, true, nil
}

func (s *Store) MessageQuoteTarget(ctx context.Context, channelID, messageID, targetChannelID string) (content, quoteChannelID, quoteMessageID string, ok bool, err error) {
	links, err := s.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return "", "", "", false, err
	}
	if len(links) > 0 {
		link := links[0]
		for _, target := range links {
			if target.TargetChannelID == targetChannelID {
				return link.SourceContentSnapshot, target.TargetChannelID, target.TargetMessageID, true, nil
			}
		}
		if link.SourceChannelID == targetChannelID {
			return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
		}
		return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
	}

	row := s.db.QueryRowContext(ctx, `SELECT `+messageLinkColumns+` FROM message_links WHERE target_channel_id=? AND target_message_id=? LIMIT 1`, channelID, messageID)
	link, err := scanMessageLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	if link.TargetChannelID == targetChannelID {
		return link.SourceContentSnapshot, link.TargetChannelID, link.TargetMessageID, true, nil
	}
	if link.SourceChannelID == targetChannelID {
		return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
	}
	targets, err := s.MessageTargets(ctx, link.SourceChannelID, link.SourceMessageID)
	if err != nil {
		return "", "", "", false, err
	}
	for _, target := range targets {
		if target.TargetChannelID == targetChannelID {
			return link.SourceContentSnapshot, target.TargetChannelID, target.TargetMessageID, true, nil
		}
	}
	return link.SourceContentSnapshot, link.SourceChannelID, link.SourceMessageID, true, nil
}

// MessageHistoryBefore returns up to limit distinct source messages in
// channelIDs that precede beforeMessageID, oldest first. Context is always
// taken from the conversation as it stood when the message was posted, so
// edits and backfilled messages never see later messages.
func (s *Store) MessageHistoryBefore(ctx context.Context, channelIDs []string, beforeMessageID string, limit int) ([]MessageLink, error) {
	if limit <= 0 || len(channelIDs) == 0 {
		return nil, nil
	}
	beforeValue, err := parseDiscordSnowflakeID("before_message_id", beforeMessageID)
	if err != nil {
		return nil, err
	}
	placeholders := strings.Repeat("?,", len(channelIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(channelIDs)+2)
	for _, channelID := range channelIDs {
		args = append(args, channelID)
	}
	args = append(args, beforeValue, limit)
	query := `SELECT ` + messageLinkColumns + `
		FROM message_links
		WHERE source_channel_id IN (` + placeholders + `) AND source_message_id<?
		GROUP BY source_channel_id, source_message_id
		ORDER BY source_message_id DESC
		LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	links, err := scanMessageLinks(rows)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(links)-1; i < j; i, j = i+1, j-1 {
		links[i], links[j] = links[j], links[i]
	}
	return links, nil
}
func scanMessageLinks(rows *sql.Rows) ([]MessageLink, error) {
	defer rows.Close()
	var out []MessageLink
	for rows.Next() {
		link, err := scanMessageLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

type messageLinkScanner interface {
	Scan(dest ...any) error
}

func scanMessageLink(row messageLinkScanner) (MessageLink, error) {
	var l MessageLink
	var imagesJSON string
	if err := row.Scan(&l.SourceMessageID, &l.SourceChannelID, &l.GroupID, &l.TargetChannelID, &l.TargetMessageID, &l.TargetLanguage, &l.SourceAuthorID, &l.SourceAuthorDisplayName, &l.SourceContentSnapshot, &imagesJSON, &l.TimestampLine); err != nil {
		return MessageLink{}, err
	}
	images, err := unmarshalImageAttachments(imagesJSON)
	if err != nil {
		return MessageLink{}, err
	}
	l.SourceImageAttachments = images
	return l, nil
}

func (s *Store) GetPinState(ctx context.Context, channelID, messageID string) (pinned bool, known bool, err error) {
	messageIDValue, err := parseDiscordSnowflakeID("message_id", messageID)
	if err != nil {
		return false, false, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT pinned FROM pin_states WHERE channel_id=? AND message_id=?`, channelID, messageIDValue)
	var value int
	err = row.Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return value != 0, true, nil
}

func (s *Store) SavePinState(ctx context.Context, channelID, messageID string, pinned bool) error {
	messageIDValue, err := parseDiscordSnowflakeID("message_id", messageID)
	if err != nil {
		return err
	}
	value := 0
	if pinned {
		value = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO pin_states(channel_id,message_id,pinned) VALUES(?,?,?)`, channelID, messageIDValue, value)
	return err
}

// PurgeMessageLinksTranslatedBefore deletes links whose mirror was posted
// before cutoff. The mirror message snowflake is the translation time, so a
// backfilled copy of an old message is kept as long as a fresh translation.
func (s *Store) PurgeMessageLinksTranslatedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	maxID := snowflakeIDBefore(cutoff.UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM message_links WHERE target_message_id < ?`, maxID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if err := deleteOrphanedMessageReferences(ctx, tx); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pin_states WHERE NOT EXISTS (
		SELECT 1 FROM message_links ml
		WHERE (ml.source_channel_id=pin_states.channel_id AND ml.source_message_id=pin_states.message_id)
			OR (ml.target_channel_id=pin_states.channel_id AND ml.target_message_id=pin_states.message_id)
	)`); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
