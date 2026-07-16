package main

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type retentionStoreStub struct {
	mu             sync.Mutex
	messageCutoffs []time.Time
	guildCutoffs   []time.Time
	guildPurged    chan struct{}
	guildIDs       []string
	purgeStarted   chan string
	releasePurge   map[string]chan struct{}
	state          *discordgo.State
	stateWriteLock []string
	canceled       []string
}

func (s *retentionStoreStub) PurgeMessageLinksOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	s.messageCutoffs = append(s.messageCutoffs, cutoff)
	return 0, nil
}

func (s *retentionStoreStub) GuildIDsRemovedBefore(_ context.Context, cutoff time.Time) ([]string, error) {
	s.guildCutoffs = append(s.guildCutoffs, cutoff)
	return append([]string(nil), s.guildIDs...), nil
}

func (s *retentionStoreStub) PurgeGuildRemovedBefore(_ context.Context, guildID string, _ time.Time) (bool, error) {
	if s.state != nil {
		writeLock := s.state.TryLock()
		if writeLock {
			s.state.Unlock()
		}
		s.mu.Lock()
		if writeLock {
			s.stateWriteLock = append(s.stateWriteLock, guildID)
		}
		s.mu.Unlock()
	}
	if s.purgeStarted != nil {
		s.purgeStarted <- guildID
	}
	if release := s.releasePurge[guildID]; release != nil {
		<-release
	}
	if s.guildPurged != nil {
		s.guildPurged <- struct{}{}
	}
	return true, nil
}

func (s *retentionStoreStub) CancelGuildRemoval(_ context.Context, guildID string) error {
	s.canceled = append(s.canceled, guildID)
	return nil
}

func TestRunRetentionPurgeLocksStatePerDestructiveGuildPurgeAndAllowsWriterBetweenGuilds(t *testing.T) {
	state := discordgo.NewState()
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	store := &retentionStoreStub{
		guildIDs:     []string{"first", "second"},
		purgeStarted: make(chan string, 2),
		releasePurge: map[string]chan struct{}{"first": firstRelease, "second": secondRelease},
		state:        state,
	}
	done := make(chan struct{})
	go func() {
		runRetentionPurge(context.Background(), store, time.Now(), 0, 30, state, func(string, ...any) {})
		close(done)
	}()

	if guildID := <-store.purgeStarted; guildID != "first" {
		t.Fatalf("first purge = %q, want first", guildID)
	}
	writerAcquired := make(chan struct{})
	releaseWriter := make(chan struct{})
	go func() {
		state.Lock()
		close(writerAcquired)
		<-releaseWriter
		state.Unlock()
	}()
	deadline := time.Now().Add(2 * time.Second)
	for state.TryRLock() {
		state.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("State writer did not queue during first guild purge")
		}
		runtime.Gosched()
	}
	close(firstRelease)
	select {
	case <-writerAcquired:
	case guildID := <-store.purgeStarted:
		t.Fatalf("purge %q started before queued State writer acquired the lock", guildID)
	case <-time.After(2 * time.Second):
		t.Fatal("State writer was not permitted between guild purges")
	}
	close(releaseWriter)
	if guildID := <-store.purgeStarted; guildID != "second" {
		t.Fatalf("second purge = %q, want second", guildID)
	}
	close(secondRelease)
	<-done

	if len(store.stateWriteLock) != 0 {
		t.Fatalf("Discord State writer lock was available during guild purges: %v", store.stateWriteLock)
	}
	stateUnlocked := make(chan struct{})
	go func() {
		state.Lock()
		state.Unlock()
		close(stateUnlocked)
	}()
	select {
	case <-stateUnlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("Discord State read lock was not released after guild purges")
	}
}

func TestRunRetentionPurgeCancelsPresentGuildMarkerWithoutPurging(t *testing.T) {
	state := discordgo.NewState()
	if err := state.GuildAdd(&discordgo.Guild{ID: "present"}); err != nil {
		t.Fatal(err)
	}
	store := &retentionStoreStub{guildIDs: []string{"present"}, purgeStarted: make(chan string, 1)}
	runRetentionPurge(context.Background(), store, time.Now(), 0, 30, state, func(string, ...any) {})
	if len(store.canceled) != 1 || store.canceled[0] != "present" {
		t.Fatalf("canceled markers = %v, want [present]", store.canceled)
	}
	select {
	case guildID := <-store.purgeStarted:
		t.Fatalf("present guild %q was purged", guildID)
	default:
	}
}

func TestRunRetentionPurgeUsesIndependentRetentionSettings(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		messageDays int
		guildDays   int
		wantMessage bool
		wantGuild   bool
	}{
		{name: "both disabled"},
		{name: "message only", messageDays: 60, wantMessage: true},
		{name: "guild only", guildDays: 30, wantGuild: true},
		{name: "both enabled", messageDays: 60, guildDays: 30, wantMessage: true, wantGuild: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &retentionStoreStub{}
			runRetentionPurge(context.Background(), store, now, tt.messageDays, tt.guildDays, nil, func(string, ...any) {})
			if got := len(store.messageCutoffs); (got == 1) != tt.wantMessage {
				t.Fatalf("message purge calls = %d", got)
			}
			if got := len(store.guildCutoffs); (got == 1) != tt.wantGuild {
				t.Fatalf("guild purge calls = %d", got)
			}
			if tt.wantMessage && !store.messageCutoffs[0].Equal(now.Add(-time.Duration(tt.messageDays)*24*time.Hour)) {
				t.Fatalf("message cutoff = %v", store.messageCutoffs[0])
			}
			if tt.wantGuild && !store.guildCutoffs[0].Equal(now.Add(-time.Duration(tt.guildDays)*24*time.Hour)) {
				t.Fatalf("guild cutoff = %v", store.guildCutoffs[0])
			}
		})
	}
}

func TestRetentionCutoffRejectsOverflowInsteadOfReturningFutureTime(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	cutoff, err := retentionCutoff(now, 106752)
	if err == nil {
		t.Fatalf("retentionCutoff() = %v, want error", cutoff)
	}
	if cutoff.After(now) {
		t.Fatalf("overflow produced future cutoff %v after %v", cutoff, now)
	}
}

func TestRunRetentionPurgeRejectsOverflowForBothSettings(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	store := &retentionStoreStub{}
	var logs []string
	runRetentionPurge(context.Background(), store, now, 106752, 106752, nil, func(format string, _ ...any) {
		logs = append(logs, format)
	})
	if len(store.messageCutoffs) != 0 || len(store.guildCutoffs) != 0 {
		t.Fatalf("overflow reached store: message=%v guild=%v", store.messageCutoffs, store.guildCutoffs)
	}
	if len(logs) != 2 || logs[0] != "message link purge: %v" || logs[1] != "guild data purge: %v" {
		t.Fatalf("logs = %v", logs)
	}
}

func TestRunRetentionWorkerPurgesImmediatelyAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &retentionStoreStub{guildIDs: []string{"guild"}, guildPurged: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		runRetentionWorker(ctx, store, 0, 30, time.Now, nil, func(string, ...any) {})
		close(done)
	}()

	select {
	case <-store.guildPurged:
	case <-time.After(2 * time.Second):
		t.Fatal("initial guild purge did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retention worker did not stop")
	}
}

func TestRetentionWorkerStopWaitsForInFlightPurge(t *testing.T) {
	releasePurge := make(chan struct{})
	store := &retentionStoreStub{
		guildIDs:     []string{"guild"},
		purgeStarted: make(chan string, 1),
		releasePurge: map[string]chan struct{}{"guild": releasePurge},
	}
	worker := startRetentionWorker(store, 0, 30, time.Now, nil, func(string, ...any) {})
	if guildID := <-store.purgeStarted; guildID != "guild" {
		t.Fatalf("purge = %q, want guild", guildID)
	}

	stopped := make(chan struct{})
	go func() {
		worker.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned before the in-flight purge completed")
	case <-time.After(100 * time.Millisecond):
	}
	close(releasePurge)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after the in-flight purge completed")
	}
}
