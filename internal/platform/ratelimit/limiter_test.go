package ratelimit

import (
	"testing"
	"time"
)

func TestAllow_BurstPermitted(t *testing.T) {
	lim := NewLimiter(10, 3)
	// First three requests should be allowed (burst=3).
	for i := 0; i < 3; i++ {
		if !lim.Allow("k") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	// Fourth should be rejected.
	if lim.Allow("k") {
		t.Error("request 4 should be rejected (burst exhausted)")
	}
}

func TestAllow_IndependentKeys(t *testing.T) {
	lim := NewLimiter(10, 1)
	if !lim.Allow("a") {
		t.Error("key a should be allowed")
	}
	if !lim.Allow("b") {
		t.Error("key b should be allowed")
	}
	// Both exhausted now.
	if lim.Allow("a") {
		t.Error("key a should be rejected")
	}
	if lim.Allow("b") {
		t.Error("key b should be rejected")
	}
}

func TestAllow_Refill(t *testing.T) {
	lim := NewLimiter(1000, 1) // 1000 tokens/sec refills fast
	if !lim.Allow("k") {
		t.Fatal("first request should be allowed")
	}
	if lim.Allow("k") {
		t.Fatal("second request should be rejected (burst=1)")
	}
	// Wait long enough for at least 1 token to refill.
	time.Sleep(5 * time.Millisecond)
	if !lim.Allow("k") {
		t.Error("request after refill should be allowed")
	}
}

func TestAllow_TokensCappedAtBurst(t *testing.T) {
	lim := NewLimiter(100000, 2) // very high rate
	if !lim.Allow("k") {
		t.Fatal("first request should be allowed")
	}
	// Wait for tokens to refill way past burst.
	time.Sleep(10 * time.Millisecond)

	// Should still only get burst (2) tokens.
	if !lim.Allow("k") {
		t.Error("request 2 should be allowed")
	}
	if !lim.Allow("k") {
		t.Error("request 3 should be allowed")
	}
	if lim.Allow("k") {
		t.Error("request 4 should be rejected (capped at burst=2)")
	}
}
