package translatorbot

import (
	"errors"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

const discordRetryAttempts = 3

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
	for attempt := 0; attempt < discordRetryAttempts; attempt++ {
		err = fn()
		if err == nil || !isDiscordRetryable(err) || attempt == discordRetryAttempts-1 {
			return err
		}
		time.Sleep(time.Duration(1<<attempt) * 200 * time.Millisecond)
	}
	return err
}

func withDiscordRetryValue[T any](fn func() (T, error)) (T, error) {
	var (
		value T
		err   error
	)
	for attempt := 0; attempt < discordRetryAttempts; attempt++ {
		value, err = fn()
		if err == nil || !isDiscordRetryable(err) || attempt == discordRetryAttempts-1 {
			return value, err
		}
		time.Sleep(time.Duration(1<<attempt) * 200 * time.Millisecond)
	}
	return value, err
}
