package translatorbot

import (
	"context"
	"math"
	"time"
)

const (
	// backfillRequestOverheadTokens approximates the system prompt and
	// context sent with every request.
	backfillRequestOverheadTokens = 3500
	backfillRequestSeconds        = 20
	backfillPostSeconds           = 1

	// backfillEstimateArchivedThreadLimit bounds how many archived threads the
	// estimate reads. Paging every archived thread of a large server would
	// exhaust the interaction's time budget, so the estimate stops there and
	// reports its numbers as lower bounds.
	backfillEstimateArchivedThreadLimit = 500
)

// BackfillEstimate summarizes what a backfill into a new channel would do.
type BackfillEstimate struct {
	Messages     int
	Images       int
	InputTokens  int
	OutputTokens int
	Duration     time.Duration
	// Truncated reports that the history was larger than the scan reads, so
	// every count above is a lower bound.
	Truncated bool
}

// backfillEstimateSink counts the work a traversal finds instead of doing
// it. It loads no images and has no job row, so skipped and synced find
// nothing: the estimate counts what live mirroring has not carried over
// yet.
type backfillEstimateSink struct {
	est      *BackfillEstimate
	requests *int
}

func (e *backfillEstimateSink) count(m DiscordMessage) {
	e.est.Messages++
	images := len(imageAttachmentsOnly(m.Attachments))
	e.est.Images += images
	if !needsTranslation(m.Content) {
		return
	}
	e.est.InputTokens += EstimateTranslationTokens(m.Content, "") + visionTokenOverheadPerImage*min(images, visionMaxImages)
	e.est.OutputTokens += estimateMessageOutputTokens(m.Content)
}

func (e *backfillEstimateSink) skipped(context.Context, DiscordMessage) (bool, error) {
	return false, nil
}

func (e *backfillEstimateSink) synced(context.Context, DiscordMessage, mirrorDestination) (bool, error) {
	return false, nil
}

func (e *backfillEstimateSink) load(context.Context, DiscordMessage) []loadedImageAttachment {
	return nil
}

func (e *backfillEstimateSink) alone(context.Context, DiscordMessage, mirrorDestination) error {
	// The traversal flushed the batch gathered so far; the message itself
	// takes a dedicated request.
	*e.requests++
	return nil
}

func (e *backfillEstimateSink) flush(_ context.Context, items []backfillItem) error {
	if len(items) > 0 {
		*e.requests++
	}
	return nil
}

func (e *backfillEstimateSink) family(context.Context, *threadFamily) (bool, error) {
	return false, nil
}

// EstimateBackfill scans every source of the group (parent channels and
// their public threads, archived included) and estimates the work of
// backfilling them into a channel that is about to join. It walks the same
// traversal a run walks, with a sink that counts instead of posting.
func (s *Service) EstimateBackfill(ctx context.Context, guildID, groupID string) (BackfillEstimate, error) {
	channels, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
	if err != nil {
		return BackfillEstimate{}, err
	}
	var parentIDs, allIDs []string
	for _, ch := range channels {
		allIDs = append(allIDs, ch.ChannelID)
		if !isThreadOnlyChannelType(ch.ChannelType) {
			parentIDs = append(parentIDs, ch.ChannelID)
		}
	}
	var est BackfillEstimate
	var families []*threadFamily
	if len(allIDs) > 0 {
		listing, err := s.discord.PublicThreads(guildID, allIDs, true, backfillEstimateArchivedThreadLimit)
		if err != nil {
			return BackfillEstimate{}, err
		}
		est.Truncated = listing.Truncated
		families, err = groupThreadFamilies(ctx, s.store, groupID, "", listing.Threads)
		if err != nil {
			return BackfillEstimate{}, err
		}
	}
	requests := 0
	sink := &backfillEstimateSink{est: &est, requests: &requests}
	scan := func(timeline timelineSource, destFor destinationFunc) error {
		_, err := processBackfillTimeline(ctx, s, timeline, destFor, sink, func() error { return ctx.Err() })
		return err
	}
	// The parent timeline carries the pending thread creations, so a family
	// step bounds the batch in front of it exactly where a run's request
	// would end.
	if len(parentIDs) > 0 {
		reader := newTimelineReader(s.discord, guildID, parentIDs, map[string]string{})
		err := scan(newBackfillTimeline(reader, pendingThreadCreations(families, parentIDs)), func(context.Context, DiscordMessage) (mirrorDestination, bool, error) {
			return mirrorDestination{targetID: groupID}, true, nil
		})
		if err != nil {
			return BackfillEstimate{}, err
		}
	}
	for _, family := range families {
		ids := make([]string, len(family.threads))
		for i, thread := range family.threads {
			ids[i] = thread.ID
		}
		reader := newTimelineReader(s.discord, guildID, ids, map[string]string{})
		err := scan(reader, func(context.Context, DiscordMessage) (mirrorDestination, bool, error) {
			return mirrorDestination{targetID: family.threads[0].ID}, true, nil
		})
		if err != nil {
			return BackfillEstimate{}, err
		}
	}
	est.InputTokens += requests * backfillRequestOverheadTokens
	seconds := float64(requests*backfillRequestSeconds + est.Messages*backfillPostSeconds)
	if perMinute := s.rateLimiter.SharePerMinute(backfillRateLimitShare); perMinute > 0 {
		seconds = math.Max(seconds, float64(est.InputTokens+est.OutputTokens)/perMinute*60)
	}
	est.Duration = time.Duration(seconds) * time.Second
	return est, nil
}
