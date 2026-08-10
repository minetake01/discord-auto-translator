package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type guildLifecycleStore interface {
	MarkGuildRemoved(context.Context, string, time.Time) error
	CancelGuildRemoval(context.Context, string) error
	GuildIDsWithStoredData(context.Context) ([]string, error)
}

type guildLifecycleHandler struct {
	store guildLifecycleStore
	state *discordgo.State
	mu    sync.Mutex
}

type guildLifecyclePersistenceError struct {
	operation string
	err       error
}

func (e *guildLifecyclePersistenceError) Error() string {
	return fmt.Sprintf("%s: %v", e.operation, e.err)
}

func (e *guildLifecyclePersistenceError) Unwrap() error { return e.err }

func newGuildLifecycleHandler(store guildLifecycleStore, state *discordgo.State) *guildLifecycleHandler {
	return &guildLifecycleHandler{store: store, state: state}
}

func (h *guildLifecycleHandler) handleGuild(ctx context.Context, now func() time.Time, guildID string, knownPresent bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	presentInState := h.guildPresent(guildID)
	present := knownPresent || presentInState
	if err := h.persistPresence(ctx, guildID, now, present); err != nil {
		return err
	}
	latestInState := h.guildPresent(guildID)
	if latest := knownPresent || latestInState; latest != present {
		return h.persistPresence(ctx, guildID, now, latest)
	}
	return nil
}

func (h *guildLifecycleHandler) guildPresent(guildID string) bool {
	return guildPresentInState(h.state, guildID)
}

func guildPresentInState(state *discordgo.State, guildID string) bool {
	if state == nil {
		return false
	}
	state.RLock()
	defer state.RUnlock()
	return guildPresentInStateLocked(state, guildID)
}

func guildPresentInStateLocked(state *discordgo.State, guildID string) bool {
	for _, guild := range state.Ready.Guilds {
		if guild != nil && guild.ID == guildID {
			return true
		}
	}
	return false
}

func (h *guildLifecycleHandler) registerIfGuildPresent(guildID string, register func(string) error) error {
	if h.state == nil {
		return nil
	}
	h.state.RLock()
	defer h.state.RUnlock()
	if !guildPresentInStateLocked(h.state, guildID) {
		return nil
	}
	return register(guildID)
}

func (h *guildLifecycleHandler) persistPresence(ctx context.Context, guildID string, now func() time.Time, present bool) error {
	if present {
		if err := h.store.CancelGuildRemoval(ctx, guildID); err != nil {
			return &guildLifecyclePersistenceError{operation: "cancel guild removal", err: err}
		}
		return nil
	}
	if err := h.store.MarkGuildRemoved(ctx, guildID, now().UTC()); err != nil {
		return &guildLifecyclePersistenceError{operation: "mark guild removed", err: err}
	}
	return nil
}

func (h *guildLifecycleHandler) handleDelete(ctx context.Context, now func() time.Time, event *discordgo.GuildDelete) error {
	if event == nil || event.Guild == nil || event.ID == "" || event.Unavailable {
		return nil
	}
	return h.handleGuild(ctx, now, event.ID, false)
}

func (h *guildLifecycleHandler) handleCreate(ctx context.Context, now func() time.Time, register func(string) error, event *discordgo.GuildCreate) error {
	if event == nil || event.Guild == nil || event.ID == "" {
		return nil
	}
	if err := h.handleGuild(ctx, now, event.ID, false); err != nil {
		return err
	}
	if event.Unavailable {
		return nil
	}
	return h.registerIfGuildPresent(event.ID, register)
}

func (h *guildLifecycleHandler) handleReady(ctx context.Context, now func() time.Time, event *discordgo.Ready) error {
	if event == nil {
		return nil
	}
	readyGuilds := make(map[string]struct{}, len(event.Guilds))
	for _, guild := range event.Guilds {
		if guild != nil && guild.ID != "" {
			readyGuilds[guild.ID] = struct{}{}
		}
	}
	guildIDs, err := h.store.GuildIDsWithStoredData(ctx)
	if err != nil {
		return &guildLifecyclePersistenceError{operation: "list guilds with stored data", err: err}
	}
	for _, guildID := range guildIDs {
		_, presentInReady := readyGuilds[guildID]
		if err := h.handleGuild(ctx, now, guildID, presentInReady); err != nil {
			return err
		}
	}
	return nil
}

func failGuildLifecycle(operation string, err error, fatalf func(string, ...any)) {
	if err != nil {
		fatalf(operation+" lifecycle persistence: %v", err)
	}
}
