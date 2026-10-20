package translatorbot

import "testing"

func TestRateLimiterBlocksWhenExceeded(t *testing.T) {
	limiter := NewRateLimiter(100)
	if !limiter.AllowShare("guild", 60, 1) {
		t.Fatal("expected first request to be allowed")
	}
	limiter.Record("guild", 60)
	if !limiter.AllowShare("guild", 40, 1) {
		t.Fatal("expected request within limit to be allowed")
	}
	limiter.Record("guild", 40)
	if limiter.AllowShare("guild", 1, 1) {
		t.Fatal("expected request over limit to be blocked")
	}
}

func TestRateLimiterTakeBlocksWhenExceeded(t *testing.T) {
	limiter := NewRateLimiter(2)
	if !limiter.Take("203.0.113.1", 1) {
		t.Fatal("first request should be allowed")
	}
	if !limiter.Take("203.0.113.1", 1) {
		t.Fatal("second request should be allowed")
	}
	if limiter.Take("203.0.113.1", 1) {
		t.Fatal("third request should be blocked")
	}
	if !limiter.Take("203.0.113.2", 1) {
		t.Fatal("different client should be allowed")
	}
}

func TestRateLimiterAllowShareKeepsIdleBudgetAdmissible(t *testing.T) {
	limiter := NewRateLimiter(100)
	// A request larger than the share is still admitted while the window is empty.
	if !limiter.AllowShare("guild", 80, 0.25) {
		t.Fatal("expected idle window to admit an oversized request")
	}
	limiter.Record("guild", 80)
	if limiter.AllowShare("guild", 5, 0.25) {
		t.Fatal("expected used window to reject beyond share")
	}
}
