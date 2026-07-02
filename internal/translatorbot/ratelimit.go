package translatorbot

import (
	"sync"
	"time"
)

const defaultRateLimitTokensPerMinute = 100_000

type tokenUsage struct {
	at     time.Time
	tokens int
}

type TokenRateLimiter struct {
	limit  int
	mu     sync.Mutex
	usage  map[string][]tokenUsage
	window time.Duration
}

func NewTokenRateLimiter(limitPerMinute int) *TokenRateLimiter {
	if limitPerMinute <= 0 {
		limitPerMinute = defaultRateLimitTokensPerMinute
	}
	return &TokenRateLimiter{
		limit:  limitPerMinute,
		usage:  make(map[string][]tokenUsage),
		window: time.Minute,
	}
}

func (r *TokenRateLimiter) Allow(guildID string, estimatedTokens int) bool {
	if r == nil || r.limit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.pruneLocked(guildID, now)
	return r.totalLocked(guildID)+estimatedTokens <= r.limit
}

func (r *TokenRateLimiter) Record(guildID string, tokens int) {
	if r == nil || tokens <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.pruneLocked(guildID, now)
	r.usage[guildID] = append(r.usage[guildID], tokenUsage{at: now, tokens: tokens})
}

func (r *TokenRateLimiter) pruneLocked(guildID string, now time.Time) {
	entries := r.usage[guildID]
	cutoff := now.Add(-r.window)
	kept := entries[:0]
	for _, entry := range entries {
		if entry.at.After(cutoff) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		delete(r.usage, guildID)
		return
	}
	r.usage[guildID] = kept
}

func (r *TokenRateLimiter) totalLocked(guildID string) int {
	total := 0
	for _, entry := range r.usage[guildID] {
		total += entry.tokens
	}
	return total
}
