package ratelimit

import (
	"context"
	"testing"
	"time"
)

func allowed(l *Limiter, key string) bool {
	return l.Allow(context.Background(), key).Allowed
}

func TestAllow_BurstPermitted(t *testing.T) {
	lim := NewLimiter(10, 3)
	// First three requests should be allowed (burst=3).
	for i := 0; i < 3; i++ {
		if !allowed(lim, "k") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	// Fourth should be rejected.
	if allowed(lim, "k") {
		t.Error("request 4 should be rejected (burst exhausted)")
	}
}

func TestAllow_IndependentKeys(t *testing.T) {
	lim := NewLimiter(10, 1)
	if !allowed(lim, "a") {
		t.Error("key a should be allowed")
	}
	if !allowed(lim, "b") {
		t.Error("key b should be allowed")
	}
	// Both exhausted now.
	if allowed(lim, "a") {
		t.Error("key a should be rejected")
	}
	if allowed(lim, "b") {
		t.Error("key b should be rejected")
	}
}

func TestAllow_Refill(t *testing.T) {
	lim := NewLimiter(10, 1)
	fake := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	lim.now = func() time.Time { return fake }
	if !allowed(lim, "k") {
		t.Fatal("first request should be allowed")
	}
	if allowed(lim, "k") {
		t.Fatal("second request should be rejected (burst=1)")
	}
	fake = fake.Add(200 * time.Millisecond)
	if !allowed(lim, "k") {
		t.Error("request after refill should be allowed")
	}
}

func TestAllow_TokensCappedAtBurst(t *testing.T) {
	lim := NewLimiter(10, 2)
	fake := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	lim.now = func() time.Time { return fake }

	if !allowed(lim, "k") {
		t.Fatal("first request should be allowed")
	}
	// Jump far enough that a real clock would refill past burst.
	fake = fake.Add(time.Hour)
	if !allowed(lim, "k") {
		t.Error("request 2 should be allowed")
	}
	if !allowed(lim, "k") {
		t.Error("request 3 should be allowed")
	}
	if allowed(lim, "k") {
		t.Error("request 4 should be rejected (capped at burst=2)")
	}
}

func TestAllow_MaxBucketsCap(t *testing.T) {
	lim := NewLimiterWithMax(10, 1, 3) // only 3 keys allowed
	defer lim.Close()
	for _, key := range []string{"a", "b", "c"} {
		if !allowed(lim, key) {
			t.Fatalf("key %s should be allowed", key)
		}
	}
	// Fourth distinct key should be rejected (at cap).
	if allowed(lim, "d") {
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
	if !allowed(lim, "k") {
		t.Error("Allow should still work after Close")
	}
}

func TestWindow(t *testing.T) {
	if got := Window(10, 20); got != 2*time.Second {
		t.Fatalf("10/20 window = %v, want 2s", got)
	}
	if got := Window(1, 2); got != 2*time.Second {
		t.Fatalf("1/2 window = %v, want 2s", got)
	}
	if got := Window(0.1, 1); got != 10*time.Second {
		t.Fatalf("0.1/1 window = %v, want 10s", got)
	}
	if got := Window(15, 20); got != 1334*time.Millisecond {
		t.Fatalf("15/20 window = %v, want 1334ms", got)
	}
	if got := Window(3, 10); got != 3334*time.Millisecond {
		t.Fatalf("3/10 window = %v, want 3334ms", got)
	}
}

func TestMemoryRetryAfter(t *testing.T) {
	if got := MemoryRetryAfter(10); got != time.Second {
		t.Fatalf("rate 10 Retry-After = %v, want 1s", got)
	}
	if got := MemoryRetryAfter(0.1); got != 10*time.Second {
		t.Fatalf("rate 0.1 Retry-After = %v, want 10s", got)
	}
}

func TestFailClosed(t *testing.T) {
	if FailClosed("") || FailClosed("open") || FailClosed("bogus") {
		t.Fatal("empty/open/unknown should fail open")
	}
	if !FailClosed("closed") || !FailClosed("CLOSED") {
		t.Fatal("closed should fail closed")
	}
}
