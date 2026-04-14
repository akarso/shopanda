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

func TestAllow_MaxBucketsCap(t *testing.T) {
	lim := NewLimiterWithMax(10, 1, 3) // only 3 keys allowed
	defer lim.Close()
	for _, key := range []string{"a", "b", "c"} {
		if !lim.Allow(key) {
			t.Fatalf("key %s should be allowed", key)
		}
	}
	// Fourth distinct key should be rejected (at cap).
	if lim.Allow("d") {
		t.Error("key d should be rejected (maxBuckets reached)")
	}
	// Existing key should still work.
	// Key "a" was already seen (burst=1 exhausted), wait to refill isn't
	// practical here, but the bucket lookup branch itself doesn't check cap.
}

func TestClose_StopsEviction(t *testing.T) {
	lim := NewLimiter(10, 5)
	lim.Close()
	// After close, Allow should still work (just no eviction).
	if !lim.Allow("k") {
		t.Error("Allow should still work after Close")
	}
}
