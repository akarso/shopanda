package redis_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// setupRateLimiter connects a RateLimiter to a fresh miniredis instance.
func setupRateLimiter(t *testing.T, scope string, rate float64, burst int) (*miniredis.Miniredis, *goredis.Client, *inredis.RateLimiter) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	lim, err := inredis.NewRateLimiter(client, "test", scope, rate, burst, nil)
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}
	return mr, client, lim
}

func TestRateLimiter_AllowsUpToBurstThenRejects(t *testing.T) {
	_, _, lim := setupRateLimiter(t, "default", 10, 3)

	for i := 0; i < 3; i++ {
		if !lim.Allow("k") {
			t.Fatalf("request %d should be allowed (burst=3)", i+1)
		}
	}
	if lim.Allow("k") {
		t.Error("4th request should be rejected (burst exhausted)")
	}
}

func TestRateLimiter_IndependentKeys(t *testing.T) {
	_, _, lim := setupRateLimiter(t, "default", 10, 1)

	if !lim.Allow("a") {
		t.Error("key a should be allowed")
	}
	if !lim.Allow("b") {
		t.Error("key b should be allowed (independent budget from a)")
	}
	if lim.Allow("a") {
		t.Error("key a should now be rejected")
	}
	if lim.Allow("b") {
		t.Error("key b should now be rejected")
	}
}

// TestRateLimiter_IndependentScopesShareNoBudget pins that two limiters
// with different scopes (e.g. "default" vs "route:/api/v1/auth") on the
// SAME Redis backend never share a client's budget — required so a
// per-route override doesn't silently steal from (or get stolen from by)
// the default limiter.
func TestRateLimiter_IndependentScopesShareNoBudget(t *testing.T) {
	_, client, limA := setupRateLimiter(t, "default", 10, 1)
	limB, err := inredis.NewRateLimiter(client, "test", "route:/api/v1/auth", 10, 1, nil)
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}

	if !limA.Allow("shared-ip") {
		t.Fatal("limA first request should be allowed")
	}
	if !limB.Allow("shared-ip") {
		t.Error("limB should have its own independent budget for the same client key")
	}
}

// TestRateLimiter_SlidingWindowAdmitsAsOldEntriesExpire pins the actual
// point of a sliding-window LOG over a naive fixed-window counter: a
// request is admitted again as soon as the OLDEST entry ages out of the
// window, not only at a fixed window boundary — so a client can never
// get 2x the limit by timing requests around a boundary edge.
func TestRateLimiter_SlidingWindowAdmitsAsOldEntriesExpire(t *testing.T) {
	// window = burst/rate = 2/2 = 1 second.
	_, _, lim := setupRateLimiter(t, "default", 2, 2)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := base
	clock := func() time.Time { return current }
	inredis.SetRateLimiterClock(lim, clock)

	if !lim.Allow("k") { // t=0ms, count=1
		t.Fatal("request 1 should be allowed")
	}
	current = base.Add(400 * time.Millisecond)
	if !lim.Allow("k") { // t=400ms, count=2 (burst reached)
		t.Fatal("request 2 should be allowed (burst=2)")
	}
	current = base.Add(700 * time.Millisecond)
	if lim.Allow("k") { // t=700ms: both prior entries (0ms, 400ms) still within the 1s window
		t.Error("request 3 should be rejected — burst exhausted within the window")
	}
	// The FIRST entry (t=0ms) ages out of the window once we're past
	// t=1000ms, freeing exactly one slot — a fixed-window counter would
	// instead wait for the whole window to reset before admitting again.
	current = base.Add(1001 * time.Millisecond)
	if !lim.Allow("k") {
		t.Error("request at t=1001ms should be allowed — the t=0ms entry has aged out of the 1s window")
	}
	// The slot is used again immediately; a second one is not free yet
	// (the t=400ms entry doesn't age out until t=1400ms).
	if lim.Allow("k") {
		t.Error("immediate follow-up request should be rejected — only one slot freed so far")
	}
}

func TestRateLimiter_ConcurrentRequestsNearLimitDoNotOverAdmit(t *testing.T) {
	const burst = 20
	_, _, lim := setupRateLimiter(t, "default", 1000, burst)

	const workers = 100
	var admitted int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if lim.Allow("hot-key") {
				atomic.AddInt64(&admitted, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&admitted); got != burst {
		t.Fatalf("admitted = %d, want exactly %d (concurrent requests must not over-admit past the burst)", got, burst)
	}
}

// TestRateLimiter_CrossReplicaMembersDoNotCollide pins the code review
// fix: two independently-constructed RateLimiter instances (standing in
// for two real replicas, each with its own process, each starting its
// own seq counter at 0) sharing ONE Redis backend, the SAME scope, and
// the SAME client key must still enforce a combined limit — not double
// it. Without a per-instance random component in the ZSET member, both
// instances' first call at an identical millisecond produce the exact
// same member string ("<ms>-1"), so the second ZADD overwrites the
// first entry's score instead of adding a new one: only one ZSET entry
// would exist for two real admitted requests, silently undercounting
// traffic and letting more through than the shared burst allows. Forcing
// an identical clock on both instances (via SetRateLimiterClock)
// reproduces the worst case deterministically instead of depending on
// two real goroutines happening to race within the same millisecond.
func TestRateLimiter_CrossReplicaMembersDoNotCollide(t *testing.T) {
	const burst = 2
	_, client, replicaA := setupRateLimiter(t, "default", 1000, burst)
	replicaB, err := inredis.NewRateLimiter(client, "test", "default", 1000, burst, nil)
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}

	frozen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return frozen }
	inredis.SetRateLimiterClock(replicaA, clock)
	inredis.SetRateLimiterClock(replicaB, clock)

	admitted := 0
	for i := 0; i < burst+1; i++ {
		if replicaA.Allow("shared-key") {
			admitted++
		}
		if replicaB.Allow("shared-key") {
			admitted++
		}
	}
	if admitted != burst {
		t.Fatalf("admitted across both replicas = %d, want exactly %d (burst shared across replicas, not doubled by member collisions)", admitted, burst)
	}
}

func TestRateLimiter_FailsOpenOnRedisError(t *testing.T) {
	mr, client, lim := setupRateLimiter(t, "default", 10, 1)
	// Exhaust the real budget once to prove the limiter is otherwise wired
	// correctly, then break the connection so the NEXT check must fail open.
	if !lim.Allow("k") {
		t.Fatal("first request should be allowed")
	}
	mr.Close()
	_ = client.Close()

	if !lim.Allow("k") {
		t.Error("Allow should fail OPEN (return true) when Redis is unreachable, not take down the request path")
	}
}

func TestNewRateLimiter_RejectsInvalidArgs(t *testing.T) {
	_, client, _ := setupRateLimiter(t, "default", 10, 1)

	if _, err := inredis.NewRateLimiter(nil, "test", "default", 10, 1, nil); err == nil {
		t.Error("expected error for nil client")
	}
	if _, err := inredis.NewRateLimiter(client, "test", "", 10, 1, nil); err == nil {
		t.Error("expected error for empty scope")
	}
	if _, err := inredis.NewRateLimiter(client, "test", "default", 0, 1, nil); err == nil {
		t.Error("expected error for non-positive rate")
	}
	if _, err := inredis.NewRateLimiter(client, "test", "default", 10, 0, nil); err == nil {
		t.Error("expected error for non-positive burst")
	}
}
