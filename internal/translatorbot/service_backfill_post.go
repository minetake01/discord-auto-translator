package translatorbot

import (
	"context"
	"errors"
	"log"
)

// backfillPoster posts one message of a job and records what it posted: the
// mirror itself, the reactions and pin carried over from the source, and a
// visible notice where a message could not be posted at all.
type backfillPoster struct {
	s     *Service
	run   *backfillRun
	scope translationScope
	retry *backfillRetrier
	// reported remembers gap notices already posted during this run so a
	// retried thread create does not announce the same gap twice.
	reported map[string]bool
}

// single mirrors one message that cannot join a batch (a poll, a forwarded
// message, or a thread starter).
func (p *backfillPoster) single(ctx context.Context, m DiscordMessage, dest mirrorDestination, sourceLanguage string, contextFn func() TranslationContext) error {
	err := p.retry.translation(ctx, func() error {
		return p.s.mirrorMessage(ctx, m, p.run.job.GroupID, sourceLanguage, contextFn, []mirrorDestination{dest}, p.scope)
	})
	if err != nil {
		if isPermanentBackfillError(err) {
			return p.skip(ctx, dest, m, err)
		}
		return err
	}
	p.applyPostedStateTo(ctx, m, dest.targetID)
	return nil
}

// batchItem posts one translated message of a batch and records its link.
func (p *backfillPoster) batchItem(ctx context.Context, item backfillItem, translated string, alts []string) error {
	m, dest := item.m, item.dest
	content, err := p.s.mirrorContent(ctx, m, dest.targetID, dest.channel.Language, translated, imagesNotReuploaded(m.Attachments, item.loaded), dest.timestamp)
	if err == nil {
		var result WebhookSendResult
		result, err = p.s.sendMirror(ctx, m, p.run.job.GroupID, dest, content, nil, webhookFilesForImages(item.loaded, alts), m.Content)
		if err == nil {
			if err := p.applyPostedState(ctx, m, dest.targetID, result.MessageID); err != nil {
				log.Printf("backfill job %d: reactions/pins for %s: %v", p.run.job.ID, m.ID, err)
			}
			return nil
		}
	}
	if isPermanentBackfillError(err) {
		return p.skip(ctx, dest, m, err)
	}
	return err
}

// skip records a message that cannot be mirrored and posts a notice where its
// mirror would have gone, so the gap is visible in the target history.
func (p *backfillPoster) skip(ctx context.Context, dest mirrorDestination, m DiscordMessage, cause error) error {
	log.Printf("backfill job %d: skip %s/%s: %v", p.run.job.ID, m.ChannelID, m.ID, cause)
	p.gapNotice(dest, cause)
	return p.s.store.MarkBackfillSkipped(ctx, p.run.job.ID, m.ChannelID, m.ID, truncateRunes(cause.Error(), 500, ""))
}

// skipThread records and announces a source thread that cannot be created in
// the target. The notice goes to the target channel the thread belongs to.
func (p *backfillPoster) skipThread(ctx context.Context, rep DiscordThread, cause error) error {
	log.Printf("backfill job %d: skip thread %s: %v", p.run.job.ID, rep.ID, cause)
	if p.markReported("thread\x00" + rep.ID) {
		p.gapNotice(destinationForChannel(p.run.target), cause)
	}
	return p.s.store.MarkBackfillSkipped(ctx, p.run.job.ID, rep.ParentID, rep.ID, truncateRunes(cause.Error(), 500, ""))
}

func (p *backfillPoster) markReported(key string) bool {
	if p.reported == nil {
		p.reported = map[string]bool{}
	}
	if p.reported[key] {
		return false
	}
	p.reported[key] = true
	return true
}

// gapNotice posts a localized notice at the gap. The reason is truncated so
// the notice stays one short message even for verbose REST errors.
func (p *backfillPoster) gapNotice(dest mirrorDestination, cause error) {
	content := localizedUIStringf(dest.channel.Language, uiKeyBackfillSkipped, truncateRunes(cause.Error(), 300, "…"))
	if _, err := p.s.sendWebhookMessage(dest.channel, WebhookSend{Content: content, ThreadID: dest.threadID()}); err != nil {
		log.Printf("backfill job %d: post skip notice to %s: %v", p.run.job.ID, dest.targetID, err)
	}
}

// applyPostedStateTo applies reactions and pin to the mirror of m in
// targetChannelID. Failures are logged; the mirror itself is already posted.
func (p *backfillPoster) applyPostedStateTo(ctx context.Context, m DiscordMessage, targetChannelID string) {
	links, err := p.s.store.MessageTargets(ctx, m.ChannelID, m.ID)
	if err == nil {
		for _, link := range links {
			if link.TargetChannelID == targetChannelID {
				err = p.applyPostedState(ctx, m, link.TargetChannelID, link.TargetMessageID)
			}
		}
	}
	if err != nil {
		log.Printf("backfill job %d: reactions/pins for %s: %v", p.run.job.ID, m.ID, err)
	}
}

// applyPostedState gives a new mirror the reactions and pin of its source:
// every emoji on the source or its existing mirrors is added once by the bot,
// and the mirror is pinned if any copy is pinned.
func (p *backfillPoster) applyPostedState(ctx context.Context, m DiscordMessage, targetChannelID, targetMessageID string) error {
	emojis := append([]string(nil), m.Reactions...)
	pinned := m.Pinned
	links, err := p.s.store.MessageTargets(ctx, m.ChannelID, m.ID)
	if err != nil {
		return err
	}
	for _, link := range links {
		if link.TargetChannelID == targetChannelID {
			continue
		}
		mirror, err := p.s.discord.Message(link.TargetChannelID, link.TargetMessageID)
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
		if err := p.s.addReaction(targetChannelID, targetMessageID, emoji); err != nil {
			errs = append(errs, err)
		}
	}
	if pinned {
		if err := p.s.store.SavePinState(ctx, targetChannelID, targetMessageID, true); err != nil {
			return err
		}
		if err := p.s.setPinned(targetChannelID, targetMessageID, true); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
