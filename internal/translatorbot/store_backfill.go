package translatorbot

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// BackfillJob translates the untranslated history of a group into one
// target channel. Jobs in a guild run one at a time in id order; while a job
// exists its target receives no live mirrors.
type BackfillJob struct {
	ID              int64
	GuildID         string
	GroupID         string
	TargetChannelID string
	RequestedBy     string
	CreatedAt       time.Time
}

// JoinChannelWithBackfill registers the channel and its backfill job in one
// transaction so no live message can reach the target between the two.
func (s *Store) JoinChannelWithBackfill(ctx context.Context, ch GroupChannel, requestedBy string) (BackfillJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BackfillJob{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := joinChannelTx(ctx, tx, ch); err != nil {
		return BackfillJob{}, err
	}
	job := BackfillJob{
		GuildID: ch.GuildID, GroupID: ch.GroupID, TargetChannelID: ch.ChannelID,
		RequestedBy: requestedBy, CreatedAt: time.Now().UTC(),
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO backfill_jobs(guild_id,group_id,target_channel_id,requested_by,created_at) VALUES(?,?,?,?,?)`,
		job.GuildID, job.GroupID, job.TargetChannelID, job.RequestedBy, job.CreatedAt.UnixMilli())
	if err != nil {
		return BackfillJob{}, err
	}
	if job.ID, err = res.LastInsertId(); err != nil {
		return BackfillJob{}, err
	}
	return job, tx.Commit()
}

const backfillJobColumns = `id,guild_id,group_id,target_channel_id,requested_by,created_at`

func scanBackfillJob(row interface{ Scan(...any) error }) (BackfillJob, error) {
	var job BackfillJob
	var createdAt int64
	if err := row.Scan(&job.ID, &job.GuildID, &job.GroupID, &job.TargetChannelID, &job.RequestedBy, &createdAt); err != nil {
		return BackfillJob{}, err
	}
	job.CreatedAt = time.UnixMilli(createdAt).UTC()
	return job, nil
}

// BackfillJobs returns every pending job, ordered by guild then queue order.
func (s *Store) BackfillJobs(ctx context.Context) ([]BackfillJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+backfillJobColumns+` FROM backfill_jobs ORDER BY guild_id, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackfillJob
	for rows.Next() {
		job, err := scanBackfillJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// NextBackfillJob returns the guild's oldest pending job.
func (s *Store) NextBackfillJob(ctx context.Context, guildID string) (BackfillJob, bool, error) {
	job, err := scanBackfillJob(s.db.QueryRowContext(ctx, `SELECT `+backfillJobColumns+` FROM backfill_jobs WHERE guild_id=? ORDER BY id LIMIT 1`, guildID))
	if errors.Is(err, sql.ErrNoRows) {
		return BackfillJob{}, false, nil
	}
	if err != nil {
		return BackfillJob{}, false, err
	}
	return job, true, nil
}

// BackfillJobExists reports whether the job is still pending (it disappears
// when finished or when its target leaves the group).
func (s *Store) BackfillJobExists(ctx context.Context, jobID int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM backfill_jobs WHERE id=?`, jobID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// BackfillTargetHeld reports whether live mirroring into the channel (or a
// thread under it) must wait for a pending backfill job of the group.
func (s *Store) BackfillTargetHeld(ctx context.Context, groupID, targetChannelID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM backfill_jobs WHERE group_id=? AND target_channel_id=? LIMIT 1`, groupID, targetChannelID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) FinishBackfillJob(ctx context.Context, jobID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backfill_jobs WHERE id=?`, jobID)
	return err
}

// CancelBackfillJobs removes every pending job that fills the channel and
// reports how many there were. A channel can belong to several groups, so the
// stop command works on the channel, not on one group. A running worker
// notices the missing row and stops before posting anything else.
func (s *Store) CancelBackfillJobs(ctx context.Context, guildID, targetChannelID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM backfill_jobs WHERE guild_id=? AND target_channel_id=?`, guildID, targetChannelID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) MarkBackfillSkipped(ctx context.Context, jobID int64, sourceChannelID, sourceMessageID, reason string) error {
	messageID, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO backfill_skipped_messages(job_id,source_channel_id,source_message_id,reason) VALUES(?,?,?,?)`,
		jobID, sourceChannelID, messageID, reason)
	return err
}

func (s *Store) BackfillSkipped(ctx context.Context, jobID int64, sourceChannelID, sourceMessageID string) (bool, error) {
	messageID, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return false, err
	}
	var one int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM backfill_skipped_messages WHERE job_id=? AND source_channel_id=? AND source_message_id=?`, jobID, sourceChannelID, messageID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) BackfillTopicSummary(ctx context.Context, jobID int64, locationKey string) (generationID, summary string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT generation_id, summary FROM backfill_topic_summaries WHERE job_id=? AND location_key=?`, jobID, locationKey).Scan(&generationID, &summary)
	return generationID, summary, err
}

func (s *Store) UpsertBackfillTopicSummary(ctx context.Context, jobID int64, locationKey, generationID, summary string) error {
	summary = strings.TrimSpace(summary)
	if locationKey == "" || generationID == "" || summary == "" {
		return errors.New("backfill topic summary location, generation, and text are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO backfill_topic_summaries(job_id,location_key,generation_id,summary) VALUES(?,?,?,?)
		ON CONFLICT(job_id, location_key) DO UPDATE SET generation_id=excluded.generation_id, summary=excluded.summary`,
		jobID, locationKey, generationID, summary)
	return err
}

// SourceMessageMirroredTo reports whether a source message already has a
// mirror in the target channel or thread.
func (s *Store) SourceMessageMirroredTo(ctx context.Context, sourceChannelID, sourceMessageID, targetChannelID string) (bool, error) {
	messageID, err := parseDiscordSnowflakeID("source_message_id", sourceMessageID)
	if err != nil {
		return false, err
	}
	var one int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM message_links WHERE source_channel_id=? AND source_message_id=? AND target_channel_id=? LIMIT 1`,
		sourceChannelID, messageID, targetChannelID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// IsMirrorMessage reports whether channel/message is a mirror written by the
// bot (it must not be translated again as a source).
func (s *Store) IsMirrorMessage(ctx context.Context, channelID, messageID string) (bool, error) {
	messageIDValue, err := parseDiscordSnowflakeID("target_message_id", messageID)
	if err != nil {
		return false, err
	}
	var one int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM message_links WHERE target_channel_id=? AND target_message_id=? LIMIT 1`, channelID, messageIDValue).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// backfillSummaries scopes rolling topic summaries to one backfill job so
// historical summaries never overwrite or leak into live translation.
type backfillSummaries struct {
	store *Store
	jobID int64
}

func (b backfillSummaries) TopicSummary(ctx context.Context, locationKey, generationID string) (string, error) {
	gotGeneration, summary, err := b.store.BackfillTopicSummary(ctx, b.jobID, locationKey)
	if err != nil {
		return "", err
	}
	if gotGeneration != generationID {
		return "", sql.ErrNoRows
	}
	return strings.TrimSpace(summary), nil
}

func (b backfillSummaries) TopicSummaryForLocation(ctx context.Context, locationKey string) (string, string, error) {
	return b.store.BackfillTopicSummary(ctx, b.jobID, locationKey)
}

func (b backfillSummaries) UpsertTopicSummary(ctx context.Context, _ string, locationKey, generationID, summary string) error {
	return b.store.UpsertBackfillTopicSummary(ctx, b.jobID, locationKey, generationID, summary)
}
