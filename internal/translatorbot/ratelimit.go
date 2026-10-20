package translatorbot

import (
	"sync"
	"time"
)

const (
	defaultRateLimitTokensPerMinute         = 100_000
	defaultAvatarRateLimitRequestsPerMinute = 120
)

// RateLimiter enforces a per-key budget inside a sliding window. Translation
// spends tokens and the avatar endpoint spends requests; both are the same
// weighted window, so a single machine carries them.
type RateLimiter struct {
	limit  int
	mu     sync.Mutex
	usage  map[string][]rateUsage
	window time.Duration
}

type rateUsage struct {
	at     time.Time
	weight int
}

// NewRateLimiter builds a limiter that admits limitPerWindow weight units per
// key per minute. A non-positive limit disables the limiter.
func NewRateLimiter(limitPerWindow int) *RateLimiter {
	return &RateLimiter{
		limit:  limitPerWindow,
		usage:  make(map[string][]rateUsage),
		window: time.Minute,
	}
}

// AllowShare admits weight units only while the key's usage in the window
// stays within share of the limit. A request larger than the share is still
// admitted into an idle window so it cannot wait forever.
func (r *RateLimiter) AllowShare(key string, weight int, share float64) bool {
	if r == nil || r.limit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.pruneLocked(key, now)
	total := r.totalLocked(key)
	if total+weight <= int(float64(r.limit)*share) {
		return true
	}
	return total == 0 && weight <= r.limit
}

// Take admits weight units and records them in one step, for callers whose
// request is the cost itself.
func (r *RateLimiter) Take(key string, weight int) bool {
	if r == nil || r.limit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.pruneLocked(key, now)
	if r.totalLocked(key)+weight > r.limit {
		return false
	}
	r.usage[key] = append(r.usage[key], rateUsage{at: now, weight: weight})
	return true
}

// Record records weight units already spent, for callers that learn the cost
// after the fact.
func (r *RateLimiter) Record(key string, weight int) {
	if r == nil || weight <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.pruneLocked(key, now)
	r.usage[key] = append(r.usage[key], rateUsage{at: now, weight: weight})
}

// SharePerMinute is the per-key budget per minute that a caller allowed share
// of the window may plan with.
func (r *RateLimiter) SharePerMinute(share float64) float64 {
	if r == nil || r.limit <= 0 {
		return 0
	}
	return float64(r.limit) * share
}

func (r *RateLimiter) pruneLocked(key string, now time.Time) {
	entries := r.usage[key]
	cutoff := now.Add(-r.window)
	kept := entries[:0]
	for _, entry := range entries {
		if entry.at.After(cutoff) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		delete(r.usage, key)
		return
	}
	r.usage[key] = kept
}

func (r *RateLimiter) totalLocked(key string) int {
	total := 0
	for _, entry := range r.usage[key] {
		total += entry.weight
	}
	return total
}
