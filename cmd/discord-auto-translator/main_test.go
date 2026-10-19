package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeModelWarmer struct {
	warmUp func(context.Context) error
}

func (f fakeModelWarmer) WarmUp(ctx context.Context) error {
	return f.warmUp(ctx)
}

func TestParseStartupOptions(t *testing.T) {
	got, err := parseStartupOptions([]string{"--env-file", "/tmp/model.env", "--model-prewarm"})
	if err != nil {
		t.Fatal(err)
	}
	if got.envFile != "/tmp/model.env" || !got.modelPrewarm {
		t.Fatalf("options = %#v", got)
	}

	defaults, err := parseStartupOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.envFile != ".env" || defaults.modelPrewarm {
		t.Fatalf("defaults = %#v", defaults)
	}

	for _, args := range [][]string{{"--env-file", ""}, {"unexpected"}, {"--unknown"}} {
		if _, err := parseStartupOptions(args); err == nil {
			t.Fatalf("parseStartupOptions(%q) succeeded, want error", args)
		}
	}
}

func TestPrewarmModelUsesFiveMinuteDeadlineAndReturnsErrors(t *testing.T) {
	started := time.Now()
	if err := prewarmModel(context.Background(), fakeModelWarmer{warmUp: func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("prewarm context has no deadline")
		}
		if remaining := deadline.Sub(started); remaining < 4*time.Minute+59*time.Second || remaining > 5*time.Minute+time.Second {
			t.Fatalf("prewarm deadline remaining = %s", remaining)
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}

	want := errors.New("access denied")
	err := prewarmModel(context.Background(), fakeModelWarmer{warmUp: func(context.Context) error { return want }})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err = prewarmModel(canceled, fakeModelWarmer{warmUp: func(ctx context.Context) error { return ctx.Err() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}
