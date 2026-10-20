package translatorbot

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

// backfillRetrier waits out the failures that retrying the same request fixes:
// a guild over its token budget and a briefly unavailable provider. Backfill
// never drops history for those, so it waits and tries again until the job is
// gone. Every other failure goes back to the caller.
type backfillRetrier struct {
	s     *Service
	jobID int64
}

func (b *backfillRetrier) do(ctx context.Context, op func() error) error {
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
