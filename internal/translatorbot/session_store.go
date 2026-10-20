package translatorbot

import (
	"sync"
	"time"
)

// sessionStore keeps values addressed by key for a bounded time. Entries
// expire lazily: put prunes every stale entry so the map stays bounded, get
// and take drop an entry whose deadline passed.
type sessionStore[V any] struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]sessionEntry[V]
}

type sessionEntry[V any] struct {
	value     V
	expiresAt time.Time
}

func newSessionStore[V any](ttl time.Duration) *sessionStore[V] {
	return &sessionStore[V]{ttl: ttl, items: make(map[string]sessionEntry[V])}
}

func (s *sessionStore[V]) put(key string, value V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, entry := range s.items {
		if now.After(entry.expiresAt) {
			delete(s.items, id)
		}
	}
	s.items[key] = sessionEntry[V]{value: value, expiresAt: now.Add(s.ttl)}
}

func (s *sessionStore[V]) get(key string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.items, key)
		var zero V
		return zero, false
	}
	return entry.value, true
}

// take removes and returns the entry for key, reporting false when it is
// missing or expired.
func (s *sessionStore[V]) take(key string) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var zero V
	entry, ok := s.items[key]
	if ok {
		delete(s.items, key)
	}
	if !ok || time.Now().After(entry.expiresAt) {
		return zero, false
	}
	return entry.value, true
}

func (s *sessionStore[V]) delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}
