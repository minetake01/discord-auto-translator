package translatorbot

import (
	"errors"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	discordRetryAttempts = 3
	discordRetryBackoff  = 200 * time.Millisecond
)

// discordRESTError reports whether err carries a Discord REST response, and
// returns it.
func discordRESTError(err error) (*discordgo.RESTError, bool) {
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Response != nil {
		return restErr, true
	}
	return nil, false
}

func isDiscordRetryable(err error) bool {
	restErr, ok := discordRESTError(err)
	if !ok {
		return false
	}
	code := restErr.Response.StatusCode
	return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// isUnknownMessageError reports the 404 Discord returns for a message that no
// longer exists. Deleting a mirror part a moderator already removed is not a
// failure.
func isUnknownMessageError(err error) bool {
	restErr, ok := discordRESTError(err)
	return ok && restErr.Response.StatusCode == http.StatusNotFound
}

// withDiscordRetry is the application-layer retry policy shared by every
// Discord REST call in this package: 429 and 5xx responses are retried with
// exponential backoff. It sits above discordgo's own transport-level retry
// (MaxRestRetries on the session), which stays untouched.
func withDiscordRetry(fn func() error) error {
	_, err := withDiscordRetryValue(func() (struct{}, error) { return struct{}{}, fn() })
	return err
}

func withDiscordRetryValue[T any](fn func() (T, error)) (T, error) {
	var (
		value T
		err   error
	)
	for attempt := range discordRetryAttempts {
		value, err = fn()
		if err == nil || !isDiscordRetryable(err) || attempt == discordRetryAttempts-1 {
			return value, err
		}
		time.Sleep(discordRetryBackoff * time.Duration(1<<attempt))
	}
	return value, err
}
