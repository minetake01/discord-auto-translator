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

func isDiscordRetryable(err error) bool {
	var restErr *discordgo.RESTError
	if !errors.As(err, &restErr) || restErr.Response == nil {
		return false
	}
	code := restErr.Response.StatusCode
	return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

func withDiscordRetry(fn func() error) error {
	var err error
	for attempt := range discordRetryAttempts {
		err = fn()
		if err == nil || !isDiscordRetryable(err) || attempt == discordRetryAttempts-1 {
			return err
		}
		time.Sleep(discordRetryBackoff * time.Duration(1<<attempt))
	}
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
