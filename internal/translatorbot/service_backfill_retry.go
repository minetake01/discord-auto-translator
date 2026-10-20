package translatorbot

import (
	"context"
	"errors"
	"time"
)

// backfillRetrier waits out the translation failures that retrying the same
// request fixes: a guild over its token budget and a briefly unavailable
// provider. Backfill never drops history for those, so it waits and tries
// again until the job is gone. Discord failures of a wrapped step are not
// classified here: the Discord API layer retries its transient errors, and
// the caller decides what is permanent through isPermanentBackfillError.
type backfillRetrier struct {
	s     *Service
	jobID int64
}

// translation runs one mirror step and waits out its translation failures.
// The steps wrapped here mix translation with Discord work (a batch
// translation, a mirror post, a thread create); only the translation part
// returns the errors classified below — the Discord part either surfaces to
// the caller for its permanence check or was retried by the API layer.
func (b *backfillRetrier) translation(ctx context.Context, op func() error) error {
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
		if err := b.s.backfill.wait(ctx, wait); err != nil {
			return err
		}
		if err := b.active(ctx); err != nil {
			return err
		}
	}
}

// active reports errBackfillCanceled once the job row is gone, which is how a
// stopped job ends.
func (b *backfillRetrier) active(ctx context.Context) error {
	exists, err := b.s.store.BackfillJobExists(ctx, b.jobID)
	if err != nil {
		return err
	}
	if !exists {
		return errBackfillCanceled
	}
	return nil
}

// isPermanentBackfillError reports failures that retrying the same message
// cannot fix: an unusable translation, text over the Discord limit, or a
// Discord rejection the API layer's transient retry does not cover. A Discord
// REST error is either retryable or permanent, so permanence is the
// complement of isDiscordRetryable and the two tables cannot drift apart.
func isPermanentBackfillError(err error) bool {
	if errors.Is(err, errTranslationContent) || errors.Is(err, errContentTooLong) {
		return true
	}
	if _, ok := discordRESTError(err); ok {
		return !isDiscordRetryable(err)
	}
	return false
}
