package translatorbot

import (
	"context"
	"fmt"
	"slices"
)

// threadFamily is a set of already-linked threads in the source channels.
// Their messages all go into one thread of the target channel.
type threadFamily struct {
	threads        []DiscordThread // sorted by id; threads[0] represents the family
	targetThreadID string
}

// groupThreadFamilies groups source threads by the target thread they already
// share. targetChannelID picks which target's thread each family resolves to;
// empty means the group has no target yet, as when a channel is about to join.
func groupThreadFamilies(ctx context.Context, store *Store, groupID, targetChannelID string, threads []DiscordThread) ([]*threadFamily, error) {
	slices.SortFunc(threads, func(a, b DiscordThread) int { return compareSnowflakes(a.ID, b.ID) })
	byThread := map[string]*threadFamily{}
	var families []*threadFamily
	for _, thread := range threads {
		links, err := store.ThreadTargets(ctx, thread.ID)
		if err != nil {
			return nil, err
		}
		var family *threadFamily
		for _, link := range links {
			if f := byThread[link.TargetThreadID]; f != nil {
				family = f
				break
			}
		}
		if family == nil {
			family = &threadFamily{}
			families = append(families, family)
		}
		family.threads = append(family.threads, thread)
		byThread[thread.ID] = family
		for _, link := range links {
			if link.GroupID != groupID {
				continue
			}
			if link.TargetChannelID == targetChannelID {
				family.targetThreadID = link.TargetThreadID
			}
		}
	}
	return families, nil
}

// backfillThreads creates the target thread of a source thread family through
// the live thread-create path and records the resulting link.
type backfillThreads struct {
	s        *Service
	run      *backfillRun
	scope    translationScope
	channels []GroupChannel
	poster   *backfillPoster
	retry    *backfillRetrier
}

// create creates the family's thread in the target channel. For forum posts the
// starter message m becomes the initial message; the return value reports
// whether m was consumed that way.
func (t *backfillThreads) create(ctx context.Context, family *threadFamily, m *DiscordMessage) (bool, error) {
	rep := family.threads[0]
	source := findChannel(t.channels, rep.ParentID)
	if source == nil {
		return false, fmt.Errorf("thread %s parent %s is not in the group", rep.ID, rep.ParentID)
	}
	req := threadCreateRequest{
		GuildID: t.run.job.GuildID, SourceChannelID: rep.ParentID, SourceThreadID: rep.ID,
		Name: rep.Name, AppliedTags: rep.AppliedTags, OnlyTargetChannelID: t.run.target.ChannelID,
		Timestamp: true,
	}
	consumed := false
	if isThreadOnlyChannelType(source.ChannelType) {
		if m != nil && m.ID == rep.ID && m.ChannelID == rep.ID {
			req.InitialMessageID = m.ID
			req.InitialMessageAuthor = m.AuthorID
			req.InitialMessageUsername = m.AuthorDisplayName
			req.InitialMessageRoleColor = m.AuthorRoleColor
			req.InitialMessageText = m.Content
			req.InitialMessagePoll = m.Poll
			req.InitialMessageFiles = m.Attachments
			req.InitialMessageStickers = m.Stickers
			consumed = true
		}
	} else {
		req.SourceMessageID = rep.ID
	}
	err := t.retry.translation(ctx, func() error {
		_, err := t.s.syncThreadCreate(ctx, req, t.scope)
		return err
	})
	if err != nil {
		if !isPermanentBackfillError(err) {
			return false, err
		}
		return consumed, t.poster.skipThread(ctx, rep, err)
	}
	links, err := t.s.store.SourceThreadTargets(ctx, rep.ID)
	if err != nil {
		return consumed, err
	}
	for _, link := range links {
		if link.GroupID == t.run.job.GroupID && link.TargetChannelID == t.run.target.ChannelID {
			family.targetThreadID = link.TargetThreadID
		}
	}
	if consumed && family.targetThreadID != "" {
		t.poster.applyPostedStateTo(ctx, *m, family.targetThreadID)
	}
	return consumed, nil
}

// createThread creates the family's target thread where the timeline reaches
// the thread's creation, and reports whether a thread exists afterwards. The
// fresh thread takes the archive and lock state of the source threads it
// mirrors, so a family without new messages still ends up faithful.
func (r *backfillRunner) createThread(ctx context.Context, family *threadFamily) (bool, error) {
	if _, err := r.threads.create(ctx, family, nil); err != nil {
		return false, err
	}
	if family.targetThreadID == "" {
		return false, nil
	}
	rep := family.threads[0]
	if err := r.s.applyThreadState(family.targetThreadID, threadState{archived: rep.Archived, locked: rep.Locked}); err != nil {
		return false, err
	}
	return true, nil
}

// processFamily mirrors one family: its threads' new messages, the target
// thread itself, and the archive/lock state of the family.
func (r *backfillRunner) processFamily(ctx context.Context, family *threadFamily) (int, error) {
	rep := family.threads[0]
	var ids []string
	for _, thread := range family.threads {
		cursor := r.cursors[thread.ID]
		if cursor != "" && thread.LastMessageID != "" && !snowflakeIDLess(cursor, thread.LastMessageID) {
			continue
		}
		ids = append(ids, thread.ID)
	}
	seen := 0
	// Posting would let Discord unarchive the target thread by itself, and
	// that change would be synced back to the source threads; the thread is
	// opened explicitly before the first post and closed again afterwards.
	touched := false
	if len(ids) > 0 {
		reader := newTimelineReader(r.s.discord, r.run.job.GuildID, ids, r.cursors)
		n, err := processBackfillTimeline(ctx, r.s, reader, func(ctx context.Context, m DiscordMessage) (mirrorDestination, bool, error) {
			if family.targetThreadID == "" {
				consumed, err := r.threads.create(ctx, family, &m)
				if err != nil || consumed || family.targetThreadID == "" {
					touched = touched || family.targetThreadID != ""
					return mirrorDestination{}, false, err
				}
				touched = true
			}
			if !touched {
				if err := r.s.applyThreadState(family.targetThreadID, threadState{}); err != nil {
					return mirrorDestination{}, false, err
				}
				touched = true
			}
			return mirrorDestination{channel: r.run.target, targetID: family.targetThreadID, timestamp: true}, true, nil
		}, r, func() error { return r.retry.active(ctx) })
		seen += n
		if err != nil {
			return seen, err
		}
	}
	if family.targetThreadID == "" {
		if _, err := r.threads.create(ctx, family, nil); err != nil {
			return seen, err
		}
		if family.targetThreadID == "" {
			return seen, nil
		}
		touched = true
		seen++
	}
	if touched {
		if err := r.s.applyThreadState(family.targetThreadID, threadState{archived: rep.Archived, locked: rep.Locked}); err != nil {
			return seen, err
		}
	}
	return seen, nil
}
