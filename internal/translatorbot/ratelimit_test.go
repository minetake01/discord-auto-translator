package translatorbot

import "testing"

func TestTokenRateLimiterBlocksWhenExceeded(t *testing.T) {
	limiter := NewTokenRateLimiter(100)
	if !limiter.Allow("guild", 60) {
		t.Fatal("expected first request to be allowed")
	}
	limiter.Record("guild", 60)
	if !limiter.Allow("guild", 40) {
		t.Fatal("expected request within limit to be allowed")
	}
	limiter.Record("guild", 40)
	if limiter.Allow("guild", 1) {
		t.Fatal("expected request over limit to be blocked")
	}
}

func TestTokenRateLimiterUsesDefaultLimit(t *testing.T) {
	limiter := NewTokenRateLimiter(0)
	if limiter.limit != defaultRateLimitTokensPerMinute {
		t.Fatalf("got %d", limiter.limit)
	}
}
