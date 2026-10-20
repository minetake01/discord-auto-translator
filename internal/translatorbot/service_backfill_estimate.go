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

// EstimateBackfill scans every source of the group (parent channels and
// their public threads, archived included) and estimates the work of
// backfilling them into a channel that is about to join.
func (s *Service) EstimateBackfill(ctx context.Context, guildID, groupID string) (BackfillEstimate, error) {
	channels, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
	if err != nil {
		return BackfillEstimate{}, err
	}
	var est BackfillEstimate
	requests := 0
	var batch backfillBatch
	closeBatch := func() {
		if len(batch.items) > 0 {
			requests++
			batch = backfillBatch{}
		}
	}
	count := func(channelIDs []string) error {
		reader := newTimelineReader(s.discord, guildID, channelIDs, map[string]string{})
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			m, ok, err := reader.next()
			if err != nil {
				return err
			}
			if !ok {
				closeBatch()
				return nil
			}
			eligible, err := s.backfillEligible(ctx, m)
			if err != nil {
				return err
			}
			if !eligible {
				continue
			}
			est.Messages++
			images := len(imageAttachmentsOnly(m.Attachments))
			est.Images += images
			if !needsTranslation(m.Content) {
				continue
			}
			tokens := EstimateTranslationTokens(m.Content, "")
			est.InputTokens += tokens + visionTokenOverheadPerImage*min(images, visionMaxImages)
			est.OutputTokens += estimateMessageOutputTokens(m.Content)
			if backfillTranslatesAlone(m) {
				closeBatch()
				requests++
				continue
			}
			// An estimate batches into one destination: which channel each
			// message goes to does not change how many requests are sent.
			item := backfillItem{m: m}
			if !batch.fits(item) {
				closeBatch()
			}
			batch.add(item)
		}
	}
	var parentIDs, allIDs []string
	for _, ch := range channels {
		allIDs = append(allIDs, ch.ChannelID)
		if !isThreadOnlyChannelType(ch.ChannelType) {
			parentIDs = append(parentIDs, ch.ChannelID)
		}
	}
	if len(parentIDs) > 0 {
		if err := count(parentIDs); err != nil {
			return BackfillEstimate{}, err
		}
	}
	if len(allIDs) > 0 {
		listing, err := s.discord.PublicThreads(guildID, allIDs, true, backfillEstimateArchivedThreadLimit)
		if err != nil {
			return BackfillEstimate{}, err
		}
		est.Truncated = listing.Truncated
		families, err := groupThreadFamilies(ctx, s.store, groupID, "", listing.Threads)
		if err != nil {
			return BackfillEstimate{}, err
		}
		for _, family := range families {
			ids := make([]string, len(family.threads))
			for i, thread := range family.threads {
				ids[i] = thread.ID
			}
			if err := count(ids); err != nil {
				return BackfillEstimate{}, err
			}
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
