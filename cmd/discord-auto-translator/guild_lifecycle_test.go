package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type guildLifecycleStoreStub struct {
	mu            sync.Mutex
	markers       map[string]time.Time
	canceled      []string
	storedGuilds  []string
	markStarted   chan struct{}
	releaseMark   chan struct{}
	cancelStarted chan struct{}
	releaseCancel chan struct{}
	err           error
	listErr       error
}

func (s *guildLifecycleStoreStub) MarkGuildRemoved(_ context.Context, guildID string, removedAt time.Time) error {
	if s.markStarted != nil {
		close(s.markStarted)
		<-s.releaseMark
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.markers == nil {
		s.markers = map[string]time.Time{}
	}
	s.markers[guildID] = removedAt
	return s.err
}

func (s *guildLifecycleStoreStub) CancelGuildRemoval(_ context.Context, guildID string) error {
	if s.cancelStarted != nil {
		close(s.cancelStarted)
		<-s.releaseCancel
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.canceled = append(s.canceled, guildID)
	delete(s.markers, guildID)
	return s.err
}

func (s *guildLifecycleStoreStub) GuildIDsWithStoredData(context.Context) ([]string, error) {
	return append([]string(nil), s.storedGuilds...), s.listErr
}

func (s *guildLifecycleStoreStub) marked(guildID string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removedAt, ok := s.markers[guildID]
	return removedAt, ok
}

func TestHandleGuildDeleteMarksOnlyConfirmedRemoval(t *testing.T) {
	location := time.FixedZone("test", 9*60*60)
	now := time.Date(2026, time.July, 16, 13, 0, 0, 0, location)
	tests := []struct {
		name  string
		event *discordgo.GuildDelete
		want  string
	}{
		{name: "nil event"},
		{name: "nil guild", event: &discordgo.GuildDelete{}},
		{name: "empty guild id", event: &discordgo.GuildDelete{Guild: &discordgo.Guild{}}},
		{name: "temporarily unavailable", event: &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "guild", Unavailable: true}}},
		{name: "confirmed removal", event: &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "guild"}}, want: "guild"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &guildLifecycleStoreStub{}
			handler := newGuildLifecycleHandler(store, discordgo.NewState())
			if err := handler.handleDelete(context.Background(), func() time.Time { return now }, tt.event); err != nil {
				t.Fatal(err)
			}
			removedAt, marked := store.marked(tt.want)
			if (tt.want != "") != marked {
				t.Fatalf("marked = %v, want guild %q", marked, tt.want)
			}
			if marked && !removedAt.Equal(now.UTC()) {
				t.Fatalf("marked at = %v, want %v", removedAt, now.UTC())
			}
		})
	}
}

func TestGuildLifecycleConvergesWhenOlderDeleteFinishesAfterNewerCreate(t *testing.T) {
	store := &guildLifecycleStoreStub{markStarted: make(chan struct{}), releaseMark: make(chan struct{})}
	state := discordgo.NewState()
	handler := newGuildLifecycleHandler(store, state)

	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- handler.handleDelete(context.Background(), func() time.Time { return time.Unix(1, 0) }, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "guild"}})
	}()
	<-store.markStarted
	if err := state.GuildAdd(&discordgo.Guild{ID: "guild"}); err != nil {
		t.Fatal(err)
	}
	createDone := make(chan error, 1)
	go func() {
		createDone <- handler.handleCreate(context.Background(), time.Now, func(string) error { return nil }, &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "guild"}})
	}()
	close(store.releaseMark)
	if err := <-deleteDone; err != nil {
		t.Fatal(err)
	}
	if err := <-createDone; err != nil {
		t.Fatal(err)
	}
	if _, marked := store.marked("guild"); marked {
		t.Fatal("present guild remains marked for purge")
	}
}

func TestGuildLifecycleConvergesWhenOlderCreateFinishesAfterNewerDelete(t *testing.T) {
	store := &guildLifecycleStoreStub{
		markers:       map[string]time.Time{"guild": time.Unix(0, 0)},
		cancelStarted: make(chan struct{}),
		releaseCancel: make(chan struct{}),
	}
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{ID: "guild"}); err != nil {
		t.Fatal(err)
	}
	handler := newGuildLifecycleHandler(store, state)

	registered := false
	createDone := make(chan error, 1)
	go func() {
		createDone <- handler.handleCreate(context.Background(), func() time.Time { return time.Unix(2, 0) }, func(string) error {
			registered = true
			return nil
		}, &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "guild"}})
	}()
	<-store.cancelStarted
	if err := state.GuildRemove(&discordgo.Guild{ID: "guild"}); err != nil {
		t.Fatal(err)
	}
	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- handler.handleDelete(context.Background(), func() time.Time { return time.Unix(3, 0) }, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "guild"}})
	}()
	close(store.releaseCancel)
	if err := <-createDone; err != nil {
		t.Fatal(err)
	}
	if err := <-deleteDone; err != nil {
		t.Fatal(err)
	}
	if registered {
		t.Fatal("registered commands for guild no longer present in State")
	}
	if _, marked := store.marked("guild"); !marked {
		t.Fatal("absent guild is not marked for purge")
	}
}

func TestGuildLifecycleTreatsUnavailableGuildInStateAsPresent(t *testing.T) {
	store := &guildLifecycleStoreStub{markers: map[string]time.Time{"guild": time.Unix(1, 0)}}
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{ID: "guild", Unavailable: true}); err != nil {
		t.Fatal(err)
	}
	handler := newGuildLifecycleHandler(store, state)
	if err := handler.handleDelete(context.Background(), time.Now, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "guild"}}); err != nil {
		t.Fatal(err)
	}
	if _, marked := store.marked("guild"); marked {
		t.Fatal("guild present in State remains marked for purge")
	}
}

func TestHandleReadyReconcilesStoredGuildsAgainstCompleteReadySet(t *testing.T) {
	store := &guildLifecycleStoreStub{
		markers:      map[string]time.Time{"present": time.Unix(1, 0), "unavailable": time.Unix(1, 0)},
		storedGuilds: []string{"missing", "present", "unavailable"},
	}
	handler := newGuildLifecycleHandler(store, discordgo.NewState())
	now := time.Unix(2, 0)
	err := handler.handleReady(context.Background(), func() time.Time { return now }, &discordgo.Ready{Guilds: []*discordgo.Guild{
		{ID: "present"},
		{ID: "unavailable", Unavailable: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, guildID := range []string{"present", "unavailable"} {
		if _, marked := store.marked(guildID); marked {
			t.Fatalf("READY guild %q was marked", guildID)
		}
	}
	removedAt, marked := store.marked("missing")
	if !marked || !removedAt.Equal(now.UTC()) {
		t.Fatalf("missing guild marker = %v, %v", removedAt, marked)
	}
}

func TestHandleReadyDoesNotMarkGuildPresentInCurrentState(t *testing.T) {
	store := &guildLifecycleStoreStub{storedGuilds: []string{"current"}}
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{ID: "current"}); err != nil {
		t.Fatal(err)
	}
	handler := newGuildLifecycleHandler(store, state)
	if err := handler.handleReady(context.Background(), time.Now, &discordgo.Ready{}); err != nil {
		t.Fatal(err)
	}
	if _, marked := store.marked("current"); marked {
		t.Fatal("guild present in current State was marked")
	}
}

func TestGuildLifecyclePersistenceFailureIsFatal(t *testing.T) {
	want := errors.New("database is locked")
	tests := []struct {
		name    string
		present bool
		run     func(*guildLifecycleHandler) error
	}{
		{name: "delete marker", run: func(handler *guildLifecycleHandler) error {
			return handler.handleDelete(context.Background(), time.Now, &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "guild"}})
		}},
		{name: "rejoin cancellation", present: true, run: func(handler *guildLifecycleHandler) error {
			return handler.handleCreate(context.Background(), time.Now, func(string) error {
				t.Fatal("registered commands after failed lifecycle persistence")
				return nil
			}, &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "guild"}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := discordgo.NewState()
			if tt.present {
				if err := state.GuildAdd(&discordgo.Guild{ID: "guild"}); err != nil {
					t.Fatal(err)
				}
			}
			handler := newGuildLifecycleHandler(&guildLifecycleStoreStub{err: want}, state)
			err := tt.run(handler)
			var persistenceErr *guildLifecyclePersistenceError
			if !errors.As(err, &persistenceErr) {
				t.Fatalf("error = %v, want lifecycle persistence error", err)
			}
			called := false
			failGuildLifecycle(tt.name, err, func(_ string, args ...any) {
				called = true
				if got := args[0]; !errors.Is(got.(error), want) {
					t.Fatalf("fatal error = %v, want %v", got, want)
				}
			})
			if !called {
				t.Fatal("lifecycle persistence failure was discarded")
			}
		})
	}
}

func TestHandleReadyFailsOnStoredGuildLookupError(t *testing.T) {
	want := errors.New("query failed")
	handler := newGuildLifecycleHandler(&guildLifecycleStoreStub{listErr: want}, discordgo.NewState())
	err := handler.handleReady(context.Background(), time.Now, &discordgo.Ready{})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestHandleGuildCreateCancelsRemovalBeforeRegisteringCommands(t *testing.T) {
	tests := []struct {
		name         string
		event        *discordgo.GuildCreate
		wantRegister string
		wantCancel   bool
	}{
		{name: "nil event"},
		{name: "nil guild", event: &discordgo.GuildCreate{}},
		{name: "empty guild id", event: &discordgo.GuildCreate{Guild: &discordgo.Guild{}}},
		{name: "unavailable", event: &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "guild", Unavailable: true}}, wantCancel: true},
		{name: "available", event: &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "guild"}}, wantRegister: "guild", wantCancel: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &guildLifecycleStoreStub{}
			state := discordgo.NewState()
			if tt.event != nil && tt.event.Guild != nil && tt.event.ID != "" {
				if err := state.GuildAdd(&discordgo.Guild{ID: tt.event.ID}); err != nil {
					t.Fatal(err)
				}
			}
			handler := newGuildLifecycleHandler(store, state)
			registered := ""
			register := func(guildID string) error {
				if len(store.canceled) != 1 || store.canceled[0] != guildID {
					return fmt.Errorf("registered before cancellation: canceled=%v guild=%q", store.canceled, guildID)
				}
				registered = guildID
				return nil
			}
			if err := handler.handleCreate(context.Background(), time.Now, register, tt.event); err != nil {
				t.Fatal(err)
			}
			if registered != tt.wantRegister {
				t.Fatalf("registered guild = %q, want %q", registered, tt.wantRegister)
			}
			if got := len(store.canceled) > 0; got != tt.wantCancel {
				t.Fatalf("canceled = %v, want %v", store.canceled, tt.wantCancel)
			}
		})
	}
}

func TestHandleGuildCreateHoldsStateReadLockAcrossFinalPresenceCheckAndRegistration(t *testing.T) {
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{ID: "guild"}); err != nil {
		t.Fatal(err)
	}
	handler := newGuildLifecycleHandler(&guildLifecycleStoreStub{}, state)

	err := handler.handleCreate(context.Background(), time.Now, func(string) error {
		if state.TryLock() {
			state.Unlock()
			t.Fatal("Discord State writer lock was available during command registration")
		}
		return nil
	}, &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "guild"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHandleGuildCreateDoesNotClassifyRegistrationFailureAsPersistenceCorruption(t *testing.T) {
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{ID: "guild"}); err != nil {
		t.Fatal(err)
	}
	want := errors.New("Discord API unavailable")
	handler := newGuildLifecycleHandler(&guildLifecycleStoreStub{}, state)
	err := handler.handleCreate(context.Background(), time.Now, func(string) error {
		return want
	}, &discordgo.GuildCreate{Guild: &discordgo.Guild{ID: "guild"}})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	var persistenceErr *guildLifecyclePersistenceError
	if errors.As(err, &persistenceErr) {
		t.Fatalf("registration error was classified as lifecycle persistence corruption: %v", err)
	}
}
