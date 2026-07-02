package translatorbot

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestIsDiscordRetryable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{name: "nil", err: nil, retry: false},
		{name: "generic", err: errors.New("boom"), retry: false},
		{name: "429", err: &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusTooManyRequests}}, retry: true},
		{name: "500", err: &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusInternalServerError}}, retry: true},
		{name: "400", err: &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusBadRequest}}, retry: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDiscordRetryable(tc.err); got != tc.retry {
				t.Fatalf("isDiscordRetryable() = %v, want %v", got, tc.retry)
			}
		})
	}
}

func TestWithDiscordRetryRetriesTransientErrors(t *testing.T) {
	attempts := 0
	err := withDiscordRetry(func() error {
		attempts++
		if attempts < 3 {
			return &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusInternalServerError}}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestWithDiscordRetryStopsOnPermanentError(t *testing.T) {
	attempts := 0
	err := withDiscordRetry(func() error {
		attempts++
		return &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusBadRequest}}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
