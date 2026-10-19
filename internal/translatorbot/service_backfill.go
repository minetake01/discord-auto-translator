package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
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
// group or the group was deleted).
var errBackfillCanceled = errors.New("backfill job canceled")

type backfillRunKey struct{}

// backfillRun marks a context as running a backfill job. Code shared with
// live mirroring uses it to scope topic summaries to the job, to skip
// source-channel notices, and to bypass the hold on the job's own target.
type backfillRun struct {
	job       BackfillJob
	target    GroupChannel
	summaries backfillSummaries
}

func withBackfillRun(ctx context.Context, run *backfillRun) context.Context {
	return context.WithValue(ctx, backfillRunKey{}, run)
}

func backfillRunFrom(ctx context.Context) *backfillRun {
	run, _ := ctx.Value(backfillRunKey{}).(*backfillRun)
	return run
}

// backfillWorkers runs one goroutine per guild that drains the guild's job
// queue in order, and owns the hold locks that serialize the live-path
// decision with a job's final catch-up.
type backfillWorkers struct {
	mu      sync.Mutex
	running map[string]bool
	holds   sync.Map // groupID\x00targetChannelID → *sync.Mutex
	sleep   func(context.Context, time.Duration) error
}

func (w *backfillWorkers) holdLock(groupID, targetChannelID string) func() {
	actual, _ := w.holds.LoadOrStore(groupID+"\x00"+targetChannelID, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
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
func (s *Service) liveTargetHeld(ctx context.Context, groupID, targetChannelID string) (bool, error) {
	if run := backfillRunFrom(ctx); run != nil && run.job.GroupID == groupID && run.job.TargetChannelID == targetChannelID {
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
		s.backfill.mu.Lock()
		job, ok, err := s.store.NextBackfillJob(ctx, guildID)
		if err == nil && !ok {
			delete(s.backfill.running, guildID)
			s.backfill.mu.Unlock()
			return
		}
		s.backfill.mu.Unlock()
		if err == nil {
			err = s.runBackfillJob(ctx, job)
		}
		if err != nil && !errors.Is(err, errBackfillCanceled) {
			log.Printf("backfill job %d (guild %s, target %s): %v", job.ID, guildID, job.TargetChannelID, err)
			_ = s.backfill.wait(ctx, backfillErrorWait)
		}
	}
}

func (s *Service) runBackfillJob(ctx context.Context, job BackfillJob) error {
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
	r := &backfillRunner{s: s, run: run, channels: channels, cursors: map[string]string{}}
	ctx = withBackfillRun(ctx, run)
	for {
		seen, err := r.pass(ctx)
		if err != nil {
			return err
		}
		if seen > 0 {
			continue
		}
		done, err := r.finish(ctx)
		if err != nil || done {
			return err
		}
	}
}

// backfillRunner walks the source history of one job. Cursors remember the
// newest message read per source channel or thread during this run, so the
// catch-up passes only read what arrived since.
type backfillRunner struct {
	s        *Service
	run      *backfillRun
	channels []GroupChannel
	cursors  map[string]string
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

func (r *backfillRunner) ensureActive(ctx context.Context) error {
	exists, err := r.s.store.BackfillJobExists(ctx, r.run.job.ID)
	if err != nil {
		return err
	}
	if !exists {
		return errBackfillCanceled
	}
	return nil
}

// pass processes parent channels first (merged by posting order), then every
// thread family in creation order. It returns how many new source messages
// or threads it saw.
func (r *backfillRunner) pass(ctx context.Context) (int, error) {
	if err := r.ensureActive(ctx); err != nil {
		return 0, err
	}
	var parentIDs, allIDs []string
	for _, ch := range r.sources() {
		allIDs = append(allIDs, ch.ChannelID)
		if !isThreadOnlyChannelType(ch.ChannelType) {
			parentIDs = append(parentIDs, ch.ChannelID)
		}
	}
	seen := 0
	if len(parentIDs) > 0 {
		reader := newTimelineReader(r.s.discord, r.run.job.GuildID, parentIDs, r.cursors)
		n, err := r.processTimeline(ctx, reader, func(context.Context, DiscordMessage) (mirrorDestination, bool, error) {
			return mirrorDestination{channel: r.run.target, targetID: r.run.target.ChannelID, timestamp: true}, true, nil
		})
		seen += n
		if err != nil {
			return seen, err
		}
	}
	if len(allIDs) == 0 {
		return seen, nil
	}
	threads, err := r.s.discord.PublicThreads(r.run.job.GuildID, allIDs, true)
	if err != nil {
		return seen, err
	}
	families, err := r.threadFamilies(ctx, threads)
	if err != nil {
		return seen, err
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

// threadFamily is a set of already-linked threads in the source channels.
// Their messages all go into one thread of the target channel.
type threadFamily struct {
	threads        []DiscordThread // sorted by id; threads[0] represents the family
	targetThreadID string
}

func (r *backfillRunner) threadFamilies(ctx context.Context, threads []DiscordThread) ([]*threadFamily, error) {
	slices.SortFunc(threads, func(a, b DiscordThread) int { return compareSnowflakes(a.ID, b.ID) })
	byThread := map[string]*threadFamily{}
	var families []*threadFamily
	for _, thread := range threads {
		links, err := r.s.store.ThreadTargets(ctx, thread.ID)
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
			if link.GroupID != r.run.job.GroupID {
				continue
			}
			if link.TargetChannelID == r.run.target.ChannelID {
				family.targetThreadID = link.TargetThreadID
			}
		}
	}
	return families, nil
}

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
		n, err := r.processTimeline(ctx, reader, func(ctx context.Context, m DiscordMessage) (mirrorDestination, bool, error) {
			if family.targetThreadID == "" {
				consumed, err := r.createTargetThread(ctx, family, &m)
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
		})
		seen += n
		if err != nil {
			return seen, err
		}
	}
	if family.targetThreadID == "" {
		if _, err := r.createTargetThread(ctx, family, nil); err != nil {
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

// createTargetThread creates the family's thread in the target channel
// through the live thread-create path. For forum posts the starter message
// m becomes the initial message; the return value reports whether m was
// consumed that way.
func (r *backfillRunner) createTargetThread(ctx context.Context, family *threadFamily, m *DiscordMessage) (bool, error) {
	rep := family.threads[0]
	source := findChannel(r.channels, rep.ParentID)
	if source == nil {
		return false, fmt.Errorf("thread %s parent %s is not in the group", rep.ID, rep.ParentID)
	}
	req := threadCreateRequest{
		GuildID: r.run.job.GuildID, SourceChannelID: rep.ParentID, SourceThreadID: rep.ID,
		Name: rep.Name, AppliedTags: rep.AppliedTags, OnlyTargetChannelID: r.run.target.ChannelID,
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
	err := r.retryTransient(ctx, func() error {
		_, err := r.s.syncThreadCreate(ctx, req)
		return err
	})
	if err != nil {
		if !isPermanentBackfillError(err) {
			return false, err
		}
		log.Printf("backfill job %d: create thread for %s: %v", r.run.job.ID, rep.ID, err)
		if consumed {
			return true, r.skip(ctx, *m, err)
		}
		return false, nil
	}
	links, err := r.s.store.SourceThreadTargets(ctx, rep.ID)
	if err != nil {
		return consumed, err
	}
	for _, link := range links {
		if link.GroupID == r.run.job.GroupID && link.TargetChannelID == r.run.target.ChannelID {
			family.targetThreadID = link.TargetThreadID
		}
	}
	if consumed && family.targetThreadID != "" {
		r.applyPostedStateTo(ctx, *m, family.targetThreadID)
	}
	return consumed, nil
}

// destinationFunc resolves where a message of the timeline is posted; ok is
// false when the message was consumed or cannot be posted.
type destinationFunc func(context.Context, DiscordMessage) (mirrorDestination, bool, error)

// processTimeline reads a merged timeline in posting order and mirrors
// every eligible message, batching consecutive normal messages.
func (r *backfillRunner) processTimeline(ctx context.Context, reader *timelineReader, destFor destinationFunc) (int, error) {
	seen := 0
	var batch backfillBatch
	flush := func() error {
		items := batch.items
		batch = backfillBatch{}
		return r.flushBatch(ctx, items)
	}
	for {
		m, ok, err := reader.next()
		if err != nil {
			return seen, err
		}
		if !ok {
			break
		}
		seen++
		eligible, err := r.s.backfillEligible(ctx, m)
		if err != nil {
			return seen, err
		}
		if !eligible {
			continue
		}
		if skipped, err := r.s.store.BackfillSkipped(ctx, r.run.job.ID, m.ChannelID, m.ID); err != nil {
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
		synced, err := r.s.targetAlreadySynced(ctx, m.ChannelID, m.ID, dest.targetID)
		if err != nil {
			return seen, err
		}
		if synced {
			continue
		}
		if backfillTranslatesAlone(m) {
			if err := flush(); err != nil {
				return seen, err
			}
			if err := r.mirrorSingle(ctx, m, dest); err != nil {
				return seen, err
			}
			continue
		}
		item := backfillItem{m: m, dest: dest, loaded: r.s.loadImageAttachments(ctx, imageAttachmentsOnly(m.Attachments))}
		if !batch.fits(item) {
			if err := flush(); err != nil {
				return seen, err
			}
		}
		batch.add(item)
	}
	return seen, flush()
}

// backfillEligible applies the same source policy as live mirroring and
// excludes the bot's own mirrors.
func (s *Service) backfillEligible(ctx context.Context, m DiscordMessage) (bool, error) {
	if m.ThreadSystemMessage || m.ThreadStarterMessage || messageHasNoPayload(m) {
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

func messageHasNoPayload(m DiscordMessage) bool {
	return strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 && len(m.Stickers) == 0 &&
		m.ReferencedMessageID == "" && m.ForwardedMessage == nil && m.Poll == nil && m.PollResult == nil
}

// backfillTranslatesAlone lists the messages that keep their dedicated
// translation path instead of joining a batch.
func backfillTranslatesAlone(m DiscordMessage) bool {
	return m.Poll != nil || m.PollResult != nil || m.ForwardedMessage != nil || m.StartsThread
}

func (r *backfillRunner) mirrorSingle(ctx context.Context, m DiscordMessage, dest mirrorDestination) error {
	contextFn := r.contextFn(ctx, m)
	sourceLanguage := languageForChannel(r.channels, m.ChannelID)
	err := r.retryTransient(ctx, func() error {
		return r.s.mirrorMessage(ctx, m, r.run.job.GroupID, sourceLanguage, contextFn, []mirrorDestination{dest})
	})
	if err != nil {
		if isPermanentBackfillError(err) {
			return r.skip(ctx, m, err)
		}
		return err
	}
	r.applyPostedStateTo(ctx, m, dest.targetID)
	return nil
}

// applyPostedStateTo applies reactions and pin to the mirror of m in
// targetChannelID. Failures are logged; the mirror itself is already posted.
func (r *backfillRunner) applyPostedStateTo(ctx context.Context, m DiscordMessage, targetChannelID string) {
	links, err := r.s.store.MessageTargets(ctx, m.ChannelID, m.ID)
	if err == nil {
		for _, link := range links {
			if link.TargetChannelID == targetChannelID {
				err = r.applyPostedState(ctx, m, link.TargetChannelID, link.TargetMessageID)
			}
		}
	}
	if err != nil {
		log.Printf("backfill job %d: reactions/pins for %s: %v", r.run.job.ID, m.ID, err)
	}
}

func (r *backfillRunner) contextFn(ctx context.Context, m DiscordMessage) func() TranslationContext {
	contextChannelID, threadName := m.ChannelID, ""
	if m.ParentChannelID != "" {
		contextChannelID, threadName = m.ParentChannelID, m.ThreadName
	}
	return func() TranslationContext {
		return r.s.translationContextForMessage(ctx, m, r.run.job.GroupID, contextChannelID, m.ChannelID, threadName)
	}
}

func (r *backfillRunner) skip(ctx context.Context, m DiscordMessage, cause error) error {
	log.Printf("backfill job %d: skip %s/%s: %v", r.run.job.ID, m.ChannelID, m.ID, cause)
	return r.s.store.MarkBackfillSkipped(ctx, r.run.job.ID, m.ChannelID, m.ID, truncateRunes(cause.Error(), 500, ""))
}

// retryTransient waits out rate limits and provider outages; backfill never
// gives up on a message because the provider was briefly unavailable.
func (r *backfillRunner) retryTransient(ctx context.Context, op func() error) error {
	for {
		err := op()
		var wait time.Duration
		switch {
		case err == nil:
			return nil
		case errors.Is(err, errTranslationRateLimited):
			wait = backfillRateLimitWait
		case errors.Is(err, errTranslationProvider):
			wait = backfillProviderWait
		default:
			return err
		}
		if err := r.s.backfill.wait(ctx, wait); err != nil {
			return err
		}
		if err := r.ensureActive(ctx); err != nil {
			return err
		}
	}
}

// isPermanentBackfillError reports failures that retrying the same message
// cannot fix: an unusable translation, text over the Discord limit, or a
// Discord 4xx rejection.
func isPermanentBackfillError(err error) bool {
	if errors.Is(err, errTranslationContent) || errors.Is(err, errContentTooLong) {
		return true
	}
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Response != nil {
		code := restErr.Response.StatusCode
		return code >= 400 && code < 500 && code != http.StatusTooManyRequests
	}
	return false
}

// applyPostedState gives a new mirror the reactions and pin of its source:
// every emoji on the source or its existing mirrors is added once by the
// bot, and the mirror is pinned if any copy is pinned.
func (r *backfillRunner) applyPostedState(ctx context.Context, m DiscordMessage, targetChannelID, targetMessageID string) error {
	emojis := append([]string(nil), m.Reactions...)
	pinned := m.Pinned
	links, err := r.s.store.MessageTargets(ctx, m.ChannelID, m.ID)
	if err != nil {
		return err
	}
	for _, link := range links {
		if link.TargetChannelID == targetChannelID {
			continue
		}
		mirror, err := r.s.discord.Message(link.TargetChannelID, link.TargetMessageID)
		if err != nil {
			continue
		}
		emojis = append(emojis, mirror.Reactions...)
		pinned = pinned || mirror.Pinned
	}
	seen := map[string]bool{}
	var errs []error
	for _, emoji := range emojis {
		if seen[emoji] {
			continue
		}
		seen[emoji] = true
		if err := r.s.addReaction(targetChannelID, targetMessageID, emoji); err != nil {
			errs = append(errs, err)
		}
	}
	if pinned {
		if err := r.s.store.SavePinState(ctx, targetChannelID, targetMessageID, true); err != nil {
			return err
		}
		if err := r.s.setPinned(targetChannelID, targetMessageID, true); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
