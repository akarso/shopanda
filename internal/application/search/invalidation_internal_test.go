package search

import (
	"testing"
	"time"
)

// TestIndexUpdateSubscriber_Allow_DebounceWindow exercises allow directly
// (package-internal test) with fabricated timestamps instead of real
// sleeps, so the debounce window's exact boundary behavior is verified
// deterministically and fast.
func TestIndexUpdateSubscriber_Allow_DebounceWindow(t *testing.T) {
	s := &IndexUpdateSubscriber{lastFire: make(map[string]time.Time)}
	base := time.Now()

	if !s.allow("p1", base) {
		t.Fatal("first call for a product should be allowed")
	}
	if s.allow("p1", base.Add(time.Second)) {
		t.Fatal("a second call within the debounce window should be debounced")
	}
	if s.allow("p1", base.Add(productReindexDebounceWindow-time.Millisecond)) {
		t.Fatal("a call just under the window boundary should still be debounced")
	}
	if !s.allow("p1", base.Add(productReindexDebounceWindow+time.Millisecond)) {
		t.Fatal("a call just past the window boundary should be allowed again")
	}
}

func TestIndexUpdateSubscriber_Allow_PerProductKey(t *testing.T) {
	s := &IndexUpdateSubscriber{lastFire: make(map[string]time.Time)}
	base := time.Now()

	if !s.allow("p1", base) {
		t.Fatal("p1 should be allowed")
	}
	if !s.allow("p2", base) {
		t.Fatal("p2 should be allowed independently of p1's debounce")
	}
	if s.allow("p1", base.Add(time.Millisecond)) {
		t.Fatal("p1 should still be debounced")
	}
}

// TestIndexUpdateSubscriber_Allow_PrunesStaleEntries pins that lastFire
// doesn't grow unboundedly: an entry older than the debounce window is
// dropped on any later call, not just on reads of that same key.
func TestIndexUpdateSubscriber_Allow_PrunesStaleEntries(t *testing.T) {
	s := &IndexUpdateSubscriber{lastFire: make(map[string]time.Time)}
	base := time.Now()

	s.allow("stale", base)
	if len(s.lastFire) != 1 {
		t.Fatalf("lastFire len = %d, want 1", len(s.lastFire))
	}

	s.allow("other", base.Add(productReindexDebounceWindow+time.Millisecond))

	if _, ok := s.lastFire["stale"]; ok {
		t.Error("expected the stale entry to have been pruned")
	}
	if _, ok := s.lastFire["other"]; !ok {
		t.Error("expected the fresh entry to still be present")
	}
}
