package translatorbot

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	// backfillRateLimitShare is the part of a guild's token budget that
	// backfill may use; live translation keeps the rest.
	backfillRateLimitShare = 0.5

	backfillRateLimitWait = 10 * time.Second
	backfillProviderWait  = time.Minute
	backfillErrorWait     = time.Minute
)

// errBackfillCanceled stops a run whose job was removed (the target left the
// group, the group was deleted, or someone stopped it).
var errBackfillCanceled = errors.New("backfill job canceled")

// backfillRun is one running job: its row, the channel it fills, and the
// topic summaries it keeps to itself.
type backfillRun struct {
	job       BackfillJob
	target    GroupChannel
	summaries backfillSummaries
}

// translationScope says what a translation is for: live mirroring, or one
// backfill job. The two differ in token-budget share, failure reporting, and
// which topic summaries the context reads, so the scope travels as a parameter
// of every translation entry point instead of being hidden in the context.
type translationScope struct {
	// backfill is the running job; nil means live translation.
	backfill *backfillRun
}

// liveTranslation is the scope of gateway-driven mirroring.
var liveTranslation = translationScope{}

// topicSummaries returns the summary store this scope reads and writes.
func (sc translationScope) topicSummaries(store topicSummaryStore) topicSummaryStore {
	if sc.backfill != nil {
		return sc.backfill.summaries
	}
	return store
}

// fillsTarget reports whether the scope's job is what posts into this channel,
// which is why that job is not held back by its own pending row.
func (sc translationScope) fillsTarget(groupID, targetChannelID string) bool {
	run := sc.backfill
	return run != nil && run.job.GroupID == groupID && run.job.TargetChannelID == targetChannelID
}

// backfillWorkers runs one goroutine per guild that drains the guild's job
// queue in order, and owns the hold locks that serialize the live-path
// decision with a job's final catch-up.
type backfillWorkers struct {
	mu      sync.Mutex
	running map[string]bool
	// holds is keyed by group and target channel.
	holds map[string]*backfillHold
	sleep func(context.Context, time.Duration) error
}

// backfillHold is the per-target lock. Entry counts let the workers drop it
// once nobody holds or waits for it, so a long-lived process does not keep one
// lock per target channel it has ever backfilled.
type backfillHold struct {
	mu   sync.Mutex
	refs int
}

func (w *backfillWorkers) holdLock(groupID, targetChannelID string) func() {
	key := groupID + "\x00" + targetChannelID
	w.mu.Lock()
	if w.holds == nil {
		w.holds = make(map[string]*backfillHold)
	}
	hold := w.holds[key]
	if hold == nil {
		hold = &backfillHold{}
		w.holds[key] = hold
	}
	hold.refs++
	w.mu.Unlock()
	hold.mu.Lock()
	return func() {
		hold.mu.Unlock()
		w.mu.Lock()
		hold.refs--
		if hold.refs == 0 {
			delete(w.holds, key)
		}
		w.mu.Unlock()
	}
}

func (w *backfillWorkers) wait(ctx context.Context, d time.Duration) error {
	if w.sleep != nil {
		return w.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// liveTargetHeld reports whether live mirroring into the target channel (or
// a thread under it) must be skipped because a backfill job for it is
// pending. The job posts those messages itself when it catches up.
func (s *Service) liveTargetHeld(ctx context.Context, groupID, targetChannelID string, scope translationScope) (bool, error) {
	if scope.fillsTarget(groupID, targetChannelID) {
		return false, nil
	}
	unlock := s.backfill.holdLock(groupID, targetChannelID)
	defer unlock()
	return s.store.BackfillTargetHeld(ctx, groupID, targetChannelID)
}

// StartBackfill makes sure the guild's worker is draining its job queue.
func (s *Service) StartBackfill(guildID string) {
	s.backfill.mu.Lock()
	defer s.backfill.mu.Unlock()
	if s.backfill.running == nil {
		s.backfill.running = make(map[string]bool)
	}
	if s.backfill.running[guildID] {
		return
	}
	s.backfill.running[guildID] = true
	go s.runBackfillQueue(guildID)
}

// ResumeBackfills restarts the workers of every guild with pending jobs.
func (s *Service) ResumeBackfills(ctx context.Context) error {
	jobs, err := s.store.BackfillJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		s.StartBackfill(job.GuildID)
	}
	return nil
}

func (s *Service) runBackfillQueue(guildID string) {
	ctx := context.Background()
	for {
		// The queue flag is cleared in the same critical section that finds
		// no job, so a job queued right now always starts a worker.
		s.backfill.mu.Lock()
		job, ok, err := s.store.NextBackfillJob(ctx, guildID)
		if err == nil && !ok {
			delete(s.backfill.running, guildID)
			s.backfill.mu.Unlock()
			return
		}
		s.backfill.mu.Unlock()
		if err != nil {
			log.Printf("backfill queue (guild %s): %v", guildID, err)
			_ = s.backfill.wait(ctx, backfillErrorWait)
			continue
		}
		if err := s.runBackfillJob(ctx, job); err != nil && !errors.Is(err, errBackfillCanceled) {
			log.Printf("backfill job %d (guild %s, target %s): %v", job.ID, guildID, job.TargetChannelID, err)
			_ = s.backfill.wait(ctx, backfillErrorWait)
		}
	}
}

func (s *Service) runBackfillJob(ctx context.Context, job BackfillJob) error {
	// Member names and role colors fetched while reading a job's history are
	// only needed for that job.
	defer s.discord.ForgetMemberCache()
	channels, err := s.store.ChannelsInGroup(ctx, job.GuildID, job.GroupID)
	if err != nil {
		return err
	}
	target := findChannel(channels, job.TargetChannelID)
	if target == nil {
		// The target left the group; the job row is gone with it.
		return errBackfillCanceled
	}
	run := &backfillRun{job: job, target: *target, summaries: backfillSummaries{store: s.store, jobID: job.ID}}
	r := newBackfillRunner(s, run, channels)
	for {
		seen, err := r.pass(ctx)
		if err == nil {
			if seen > 0 {
				continue
			}
			done, err := r.finish(ctx)
			if err == nil {
				if done {
					return nil
				}
				continue
			}
		}
		if errors.Is(err, errBackfillCanceled) {
			return err
		}
		// A transient failure retries in place so the runner's timeline
		// cursors survive: the next pass reads only what arrived since,
		// instead of replaying the whole history through the store's dedup.
		log.Printf("backfill job %d (guild %s, target %s): %v", job.ID, job.GuildID, job.TargetChannelID, err)
		_ = s.backfill.wait(ctx, backfillErrorWait)
	}
}

// backfillRunner walks the source history of one job. Cursors remember the
// newest message read per source channel or thread during this run, so the
// catch-up passes only read what arrived since. The work itself belongs to
// backfillThreads, backfillPoster, and backfillRetrier.
type backfillRunner struct {
	s        *Service
	run      *backfillRun
	scope    translationScope
	channels []GroupChannel
	cursors  map[string]string
	threads  *backfillThreads
	poster   *backfillPoster
	retry    *backfillRetrier
}

func newBackfillRunner(s *Service, run *backfillRun, channels []GroupChannel) *backfillRunner {
	scope := translationScope{backfill: run}
	retry := &backfillRetrier{s: s, jobID: run.job.ID}
	poster := &backfillPoster{s: s, run: run, scope: scope, retry: retry, reported: map[string]bool{}}
	threads := &backfillThreads{s: s, run: run, scope: scope, channels: channels, poster: poster, retry: retry}
	return &backfillRunner{
		s: s, run: run, scope: scope, channels: channels, cursors: map[string]string{},
		threads: threads, poster: poster, retry: retry,
	}
}

// finish runs a last pass under the hold lock. When it finds nothing new the
// job is removed in the same critical section, so a live message is either
// already in this pass or sees no pending job.
func (r *backfillRunner) finish(ctx context.Context) (bool, error) {
	unlock := r.s.backfill.holdLock(r.run.job.GroupID, r.run.job.TargetChannelID)
	defer unlock()
	seen, err := r.pass(ctx)
	if err != nil || seen > 0 {
		return false, err
	}
	return true, r.s.store.FinishBackfillJob(ctx, r.run.job.ID)
}

func (r *backfillRunner) sources() []GroupChannel {
	out := make([]GroupChannel, 0, len(r.channels))
	for _, ch := range r.channels {
		if ch.ChannelID != r.run.target.ChannelID {
			out = append(out, ch)
		}
	}
	return out
}

// pass mirrors the source channels in the order the source lived them: the
// threads a parent channel holds are created in that channel's timeline where
// their source threads were created, and the messages inside every thread
// family follow once their channel's timeline is done. It returns how many new
// source messages or threads it saw.
func (r *backfillRunner) pass(ctx context.Context) (int, error) {
	if err := r.retry.active(ctx); err != nil {
		return 0, err
	}
	var parentIDs, allIDs []string
	for _, ch := range r.sources() {
		allIDs = append(allIDs, ch.ChannelID)
		if !isThreadOnlyChannelType(ch.ChannelType) {
			parentIDs = append(parentIDs, ch.ChannelID)
		}
	}
	if len(allIDs) == 0 {
		return 0, nil
	}
	listing, err := r.s.discord.PublicThreads(r.run.job.GuildID, allIDs, true, 0)
	if err != nil {
		return 0, err
	}
	families, err := groupThreadFamilies(ctx, r.s.store, r.run.job.GroupID, r.run.target.ChannelID, listing.Threads)
	if err != nil {
		return 0, err
	}
	seen := 0
	if len(parentIDs) > 0 {
		reader := newTimelineReader(r.s.discord, r.run.job.GuildID, parentIDs, r.cursors)
		timeline := newBackfillTimeline(reader, pendingThreadCreations(families, parentIDs))
		n, err := processBackfillTimeline(ctx, r.s, timeline, func(context.Context, DiscordMessage) (mirrorDestination, bool, error) {
			return mirrorDestination{channel: r.run.target, targetID: r.run.target.ChannelID, timestamp: true}, true, nil
		}, r, func() error { return r.retry.active(ctx) })
		seen += n
		if err != nil {
			return seen, err
		}
	}
	for _, family := range families {
		n, err := r.processFamily(ctx, family)
		seen += n
		if err != nil {
			return seen, err
		}
	}
	return seen, nil
}

// destinationFunc resolves where a message of the timeline is posted; ok is
// false when the message was consumed or cannot be posted.
type destinationFunc func(context.Context, DiscordMessage) (mirrorDestination, bool, error)

// backfillWorkSink receives the work a timeline traversal finds. The real
// run posts and records what it passes; an estimate only counts.
type backfillWorkSink interface {
	// count receives every message the source policy accepts.
	count(m DiscordMessage)
	// skipped reports whether the job already recorded the message as
	// undeliverable, before any destination is resolved.
	skipped(ctx context.Context, m DiscordMessage) (bool, error)
	// synced reports whether the message already reached the destination.
	synced(ctx context.Context, m DiscordMessage, dest mirrorDestination) (bool, error)
	// load gathers a batch item's image attachments.
	load(ctx context.Context, m DiscordMessage) []loadedImageAttachment
	// alone handles a message that cannot join a batch. The traversal has
	// flushed the batch gathered so far first.
	alone(ctx context.Context, m DiscordMessage, dest mirrorDestination) error
	// flush handles the batch gathered so far, if any.
	flush(ctx context.Context, items []backfillItem) error
	// family handles a thread-family step of the timeline and reports
	// whether a target thread exists afterwards.
	family(ctx context.Context, family *threadFamily) (bool, error)
}

// processBackfillTimeline reads a timeline in posting order, mirrors every
// eligible message and creates every thread where the timeline reaches it.
// Consecutive normal messages share one translation request. live is
// consulted before every step, so a canceled job or an expired scan stops
// between messages rather than at the end of a channel that may hold
// thousands of them.
func processBackfillTimeline(ctx context.Context, s *Service, timeline timelineSource, destFor destinationFunc, sink backfillWorkSink, live func() error) (int, error) {
	seen := 0
	var batch backfillBatch
	flush := func() error {
		items := batch.items
		batch = backfillBatch{}
		return sink.flush(ctx, items)
	}
	for {
		if err := live(); err != nil {
			return seen, err
		}
		step, ok, err := timeline.nextStep()
		if err != nil {
			return seen, err
		}
		if !ok {
			break
		}
		if step.family != nil {
			// The thread may only be created once the messages before it are
			// posted: Discord announces it to the channel as it appears.
			if err := flush(); err != nil {
				return seen, err
			}
			created, err := sink.family(ctx, step.family)
			if err != nil {
				return seen, err
			}
			if created {
				seen++
			}
			continue
		}
		m := step.message
		seen++
		eligible, err := s.backfillEligible(ctx, m)
		if err != nil {
			return seen, err
		}
		if !eligible {
			continue
		}
		if skipped, err := sink.skipped(ctx, m); err != nil {
			return seen, err
		} else if skipped {
			continue
		}
		dest, ok, err := destFor(ctx, m)
		if err != nil {
			return seen, err
		}
		if !ok {
			continue
		}
		if synced, err := sink.synced(ctx, m, dest); err != nil {
			return seen, err
		} else if synced {
			continue
		}
		sink.count(m)
		if backfillTranslatesAlone(m) {
			if err := flush(); err != nil {
				return seen, err
			}
			if err := sink.alone(ctx, m, dest); err != nil {
				return seen, err
			}
			continue
		}
		item := backfillItem{m: m, dest: dest, loaded: sink.load(ctx, m)}
		if !batch.fits(item) {
			if err := flush(); err != nil {
				return seen, err
			}
		}
		batch.add(item)
	}
	return seen, flush()
}

// The runner is the real backfillWorkSink: it posts what the traversal finds
// and records it for its job.

func (r *backfillRunner) count(DiscordMessage) {}

func (r *backfillRunner) skipped(ctx context.Context, m DiscordMessage) (bool, error) {
	return r.s.store.BackfillSkipped(ctx, r.run.job.ID, m.ChannelID, m.ID)
}

func (r *backfillRunner) synced(ctx context.Context, m DiscordMessage, dest mirrorDestination) (bool, error) {
	return r.s.targetAlreadySynced(ctx, m.ChannelID, m.ID, dest.targetID)
}

func (r *backfillRunner) load(ctx context.Context, m DiscordMessage) []loadedImageAttachment {
	return r.s.loadImageAttachments(ctx, imageAttachmentsOnly(m.Attachments))
}

func (r *backfillRunner) alone(ctx context.Context, m DiscordMessage, dest mirrorDestination) error {
	return r.poster.single(ctx, m, dest, languageForChannel(r.channels, m.ChannelID), r.contextFn(ctx, m))
}

func (r *backfillRunner) flush(ctx context.Context, items []backfillItem) error {
	return r.flushBatch(ctx, items)
}

func (r *backfillRunner) family(ctx context.Context, family *threadFamily) (bool, error) {
	return r.createThread(ctx, family)
}

func (r *backfillRunner) contextFn(ctx context.Context, m DiscordMessage) func() TranslationContext {
	contextChannelID, threadName := m.ChannelID, ""
	if m.ParentChannelID != "" {
		contextChannelID, threadName = m.ParentChannelID, m.ThreadName
	}
	return func() TranslationContext {
		return r.s.translationContextForMessage(ctx, m, r.run.job.GroupID, contextChannelID, m.ChannelID, threadName, r.scope)
	}
}

// backfillEligible applies the same source policy as live mirroring and
// excludes the bot's own mirrors.
func (s *Service) backfillEligible(ctx context.Context, m DiscordMessage) (bool, error) {
	if m.ThreadSystemMessage || m.ThreadStarterMessage || m.hasNoPayload() {
		return false, nil
	}
	allowed, err := s.shouldProcessMessage(ctx, m)
	if err != nil || !allowed {
		return false, err
	}
	mirror, err := s.store.IsMirrorMessage(ctx, m.ChannelID, m.ID)
	if err != nil {
		return false, err
	}
	return !mirror, nil
}

// backfillTranslatesAlone lists the messages that keep their dedicated
// translation path instead of joining a batch.
func backfillTranslatesAlone(m DiscordMessage) bool {
	return m.Poll != nil || m.PollResult != nil || m.ForwardedMessage != nil || m.StartsThread
}
