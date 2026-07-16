package main

import (
	"context"
	"fmt"
	"time"

	"discord-auto-translator/internal/translatorbot"

	"github.com/bwmarrin/discordgo"
)

type retentionStore interface {
	PurgeMessageLinksOlderThan(context.Context, time.Time) (int64, error)
	GuildIDsRemovedBefore(context.Context, time.Time) ([]string, error)
	PurgeGuildRemovedBefore(context.Context, string, time.Time) (bool, error)
	CancelGuildRemoval(context.Context, string) error
}

func runRetentionPurge(
	ctx context.Context,
	store retentionStore,
	now time.Time,
	messageLinkRetentionDays int,
	guildDataRetentionDays int,
	state *discordgo.State,
	logf func(string, ...any),
) {
	if messageLinkRetentionDays > 0 {
		cutoff, err := retentionCutoff(now, messageLinkRetentionDays)
		if err != nil {
			logf("message link purge: %v", err)
		} else {
			n, err := store.PurgeMessageLinksOlderThan(ctx, cutoff)
			if err != nil {
				logf("message link purge: %v", err)
			} else if n > 0 {
				logf("purged %d message_links older than %d days", n, messageLinkRetentionDays)
			}
		}
	}
	if guildDataRetentionDays > 0 {
		cutoff, err := retentionCutoff(now, guildDataRetentionDays)
		if err != nil {
			logf("guild data purge: %v", err)
			return
		}
		n, err := purgeGuildsRemovedBefore(ctx, store, cutoff, state)
		if err != nil {
			logf("guild data purge: %v", err)
		} else {
			logf("purged data for %d guilds removed more than %d days ago", n, guildDataRetentionDays)
		}
	}
}

func purgeGuildsRemovedBefore(ctx context.Context, store retentionStore, cutoff time.Time, state *discordgo.State) (int64, error) {
	guildIDs, err := store.GuildIDsRemovedBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	var purged int64
	for _, guildID := range guildIDs {
		didPurge, err := purgeGuildRemovedBefore(ctx, store, guildID, cutoff, state)
		if err != nil {
			return purged, err
		}
		if didPurge {
			purged++
		}
	}
	return purged, nil
}

func purgeGuildRemovedBefore(ctx context.Context, store retentionStore, guildID string, cutoff time.Time, state *discordgo.State) (bool, error) {
	if state == nil {
		return store.PurgeGuildRemovedBefore(ctx, guildID, cutoff)
	}
	state.RLock()
	defer state.RUnlock()
	if guildPresentInStateLocked(state, guildID) {
		return false, store.CancelGuildRemoval(ctx, guildID)
	}
	return store.PurgeGuildRemovedBefore(ctx, guildID, cutoff)
}

func retentionCutoff(now time.Time, days int) (time.Time, error) {
	if days <= 0 || days > translatorbot.MaxRetentionDays {
		return time.Time{}, fmt.Errorf("retention days must be between 1 and %d", translatorbot.MaxRetentionDays)
	}
	return now.UTC().Add(-time.Duration(days) * 24 * time.Hour), nil
}

func runRetentionWorker(
	ctx context.Context,
	store retentionStore,
	messageLinkRetentionDays int,
	guildDataRetentionDays int,
	now func() time.Time,
	state *discordgo.State,
	logf func(string, ...any),
) {
	run := func() {
		runRetentionPurge(ctx, store, now(), messageLinkRetentionDays, guildDataRetentionDays, state, logf)
	}
	run()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

type retentionWorker struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

func startRetentionWorker(
	store retentionStore,
	messageLinkRetentionDays int,
	guildDataRetentionDays int,
	now func() time.Time,
	state *discordgo.State,
	logf func(string, ...any),
) *retentionWorker {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runRetentionWorker(ctx, store, messageLinkRetentionDays, guildDataRetentionDays, now, state, logf)
	}()
	return &retentionWorker{cancel: cancel, done: done}
}

func (w *retentionWorker) Stop() {
	w.cancel()
	<-w.done
}
