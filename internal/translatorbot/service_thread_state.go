package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// threadStateEchoTTL bounds how long a bot-initiated archive/lock change
// waits for its THREAD_UPDATE echo.
const threadStateEchoTTL = time.Minute

type threadState struct {
	archived bool
	locked   bool
}

type threadStateEcho struct {
	state threadState
	at    time.Time
}

// threadStateTracker serializes archive/lock juggling per thread and
// remembers the states the bot itself set, so their THREAD_UPDATE echoes are
// not synced back to peer threads.
type threadStateTracker struct {
	locks sync.Map // threadID → *sync.Mutex
	mu    sync.Mutex
	echo  map[string][]threadStateEcho
}

func (t *threadStateTracker) lock(threadID string) func() {
	actual, _ := t.locks.LoadOrStore(threadID, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (t *threadStateTracker) expect(threadID string, state threadState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.echo == nil {
		t.echo = make(map[string][]threadStateEcho)
	}
	// Drop the records whose echo never arrived before adding another one, so
	// a thread the bot changes rarely does not keep them all.
	now := time.Now()
	t.echo[threadID] = append(unexpiredEchoes(t.echo[threadID], now), threadStateEcho{state: state, at: now})
}

// unexpiredEchoes keeps the records that can still match an echo.
func unexpiredEchoes(echoes []threadStateEcho, now time.Time) []threadStateEcho {
	kept := echoes[:0]
	for _, e := range echoes {
		if now.Sub(e.at) <= threadStateEchoTTL {
			kept = append(kept, e)
		}
	}
	return kept
}

// consume reports whether state is the echo of a change the bot made.
func (t *threadStateTracker) consume(threadID string, state threadState) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	pending := unexpiredEchoes(t.echo[threadID], now)
	kept := pending[:0]
	found := false
	for _, e := range pending {
		if !found && e.state == state {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(t.echo, threadID)
	} else {
		t.echo[threadID] = kept
	}
	return found
}

func (s *Service) currentThreadState(threadID string) (threadState, error) {
	ch, err := s.discord.Channel(threadID)
	if err != nil {
		return threadState{}, fmt.Errorf("fetch thread %s: %w", threadID, err)
	}
	if ch == nil || ch.ThreadMetadata == nil {
		return threadState{}, fmt.Errorf("channel %s is not a thread", threadID)
	}
	return threadState{archived: ch.ThreadMetadata.Archived, locked: ch.ThreadMetadata.Locked}, nil
}

// setThreadState moves a thread from one archive/lock state to another in a
// single channel edit.
func (s *Service) setThreadState(threadID string, from, to threadState) error {
	if from == to {
		return nil
	}
	archived, locked := to.archived, to.locked
	s.threadStates.expect(threadID, to)
	if err := s.discord.SetThreadState(threadID, &archived, &locked); err != nil {
		return fmt.Errorf("set thread %s state: %w", threadID, err)
	}
	return nil
}

// withOpenThread runs op on a message or thread in channelID. When Discord
// rejects it because the thread is archived or locked, the thread is opened,
// op is retried, and the previous state is restored.
func (s *Service) withOpenThread(channelID string, op func() error) error {
	err := op()
	if err == nil || !isThreadClosedError(err) {
		return err
	}
	unlock := s.threadStates.lock(channelID)
	defer unlock()
	prev, stateErr := s.currentThreadState(channelID)
	if stateErr != nil {
		return errors.Join(err, stateErr)
	}
	open := threadState{}
	if err := s.setThreadState(channelID, prev, open); err != nil {
		return err
	}
	opErr := op()
	if err := s.setThreadState(channelID, open, prev); err != nil {
		return errors.Join(opErr, err)
	}
	return opErr
}

// sendWebhookMessage posts a webhook message. Discord unarchives the thread on
// send by itself but rejects a locked thread, so a locked thread is unlocked
// for the send and locked again afterwards.
func (s *Service) sendWebhookMessage(target GroupChannel, send WebhookSend) (WebhookSendResult, error) {
	result, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, send)
	if err == nil || send.ThreadID == "" || !isThreadClosedError(err) {
		return result, err
	}
	unlock := s.threadStates.lock(send.ThreadID)
	defer unlock()
	prev, stateErr := s.currentThreadState(send.ThreadID)
	if stateErr != nil {
		return WebhookSendResult{}, errors.Join(err, stateErr)
	}
	if err := s.setThreadState(send.ThreadID, prev, threadState{}); err != nil {
		return WebhookSendResult{}, err
	}
	result, err = s.discord.SendWebhook(target.WebhookID, target.WebhookToken, send)
	if prev.locked {
		if lockErr := s.setThreadState(send.ThreadID, threadState{}, threadState{locked: true}); lockErr != nil {
			return result, errors.Join(err, lockErr)
		}
	}
	return result, err
}

func (s *Service) editWebhookMessage(_ context.Context, target GroupChannel, link MessageLink, content mirrorText) error {
	threadID := threadIDForWebhook(link, &target)
	return s.withOpenThread(link.TargetChannelID, func() error {
		return s.discord.EditWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID, threadID, string(content))
	})
}

func (s *Service) addReaction(channelID, messageID, emoji string) error {
	return s.withOpenThread(channelID, func() error { return s.discord.AddReaction(channelID, messageID, emoji) })
}

func (s *Service) removeOwnReaction(channelID, messageID, emoji string) error {
	return s.withOpenThread(channelID, func() error { return s.discord.RemoveOwnReaction(channelID, messageID, emoji) })
}

func (s *Service) setPinned(channelID, messageID string, pinned bool) error {
	return s.withOpenThread(channelID, func() error {
		if pinned {
			return s.discord.PinMessage(channelID, messageID)
		}
		return s.discord.UnpinMessage(channelID, messageID)
	})
}

func (s *Service) editThread(threadID, name string, appliedTags *[]string) error {
	return s.withOpenThread(threadID, func() error { return s.discord.EditThread(threadID, name, appliedTags) })
}

// SyncThreadState mirrors an archive or lock change of a thread to every
// peer thread. Changes the bot made itself are recognized and not echoed.
func (s *Service) SyncThreadState(ctx context.Context, threadID string, archived, locked bool) error {
	state := threadState{archived: archived, locked: locked}
	if s.threadStates.consume(threadID, state) {
		return nil
	}
	threads, err := s.store.ThreadTargets(ctx, threadID)
	if err != nil {
		return err
	}
	var errs []error
	for _, thread := range threads {
		if err := s.applyThreadState(thread.TargetThreadID, state); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) applyThreadState(threadID string, state threadState) error {
	unlock := s.threadStates.lock(threadID)
	defer unlock()
	current, err := s.currentThreadState(threadID)
	if err != nil {
		return err
	}
	return s.setThreadState(threadID, current, state)
}
