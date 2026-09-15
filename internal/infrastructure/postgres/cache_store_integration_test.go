package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/cache/tagtest"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

// setupCacheStore opens a test DB, runs migrations, and registers cache cleanup.
func setupCacheStore(t *testing.T) (*sql.DB, *postgres.CacheStore) {
	t.Helper()
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DELETE FROM cache_tags"); err != nil {
			t.Errorf("cleanup: delete cache_tags: %v", err)
		}
		if _, err := db.Exec("DELETE FROM cache"); err != nil {
			t.Errorf("cleanup: delete cache: %v", err)
		}
	})
	store, err := postgres.NewCacheStore(db)
	if err != nil {
		t.Fatalf("NewCacheStore: %v", err)
	}
	return db, store
}

func TestCacheStoreDB_SetAndGet(t *testing.T) {
	db, store := setupCacheStore(t)

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	if err := store.Set("k1", payload{Name: "hello", Count: 42}, 5*time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got payload
	ok, err := store.Get("k1", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Name != "hello" || got.Count != 42 {
		t.Errorf("got %+v", got)
	}

	// Verify row exists in DB.
	var raw json.RawMessage
	if err := db.QueryRow("SELECT value FROM cache WHERE key = $1", "k1").Scan(&raw); err != nil {
		t.Fatalf("DB row missing: %v", err)
	}
}

func TestCacheStoreDB_Miss(t *testing.T) {
	_, store := setupCacheStore(t)

	var dest string
	ok, err := store.Get("nonexistent", &dest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestCacheStoreDB_Upsert(t *testing.T) {
	db, store := setupCacheStore(t)

	if err := store.Set("k1", "first", 0); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := store.Set("k1", "second", 0); err != nil {
		t.Fatalf("Set second: %v", err)
	}

	var got string
	ok, err := store.Get("k1", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected hit")
	}
	if got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}

	// Verify only one row exists.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM cache WHERE key = $1", "k1").Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("rows = %d, want 1 (upsert should not duplicate)", count)
	}
}

func TestCacheStoreDB_ExpiredEntryMiss(t *testing.T) {
	db, _ := setupCacheStore(t)

	// Insert an already-expired row directly.
	past := time.Now().Add(-time.Minute)
	data, _ := json.Marshal("stale")
	if _, err := db.Exec(
		`INSERT INTO cache (key, value, expires_at) VALUES ($1, $2, $3)`,
		"expired_key", data, past,
	); err != nil {
		t.Fatalf("insert expired row: %v", err)
	}

	store, err := postgres.NewCacheStore(db)
	if err != nil {
		t.Fatalf("NewCacheStore: %v", err)
	}
	var got string
	ok, err := store.Get("expired_key", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected miss for expired entry")
	}
}

func TestCacheStoreDB_Delete(t *testing.T) {
	db, store := setupCacheStore(t)

	if err := store.Set("k1", "value", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete("k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify row removed from DB.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM cache WHERE key = $1", "k1").Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Errorf("rows = %d, want 0 after delete", count)
	}
}

func TestCacheStoreDB_DeleteMissing(t *testing.T) {
	_, store := setupCacheStore(t)

	if err := store.Delete("nonexistent"); err != nil {
		t.Fatalf("Delete missing key should not error: %v", err)
	}
}

func TestCacheStoreDB_DeleteExpired(t *testing.T) {
	db, store := setupCacheStore(t)

	// One expired, one alive, one no-TTL.
	past := time.Now().Add(-time.Minute)
	data, err := json.Marshal("x")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO cache (key, value, expires_at) VALUES ($1, $2, $3)`, "expired", data, past); err != nil {
		t.Fatalf("insert expired: %v", err)
	}
	if err := store.Set("alive", "val", time.Hour); err != nil {
		t.Fatalf("Set alive: %v", err)
	}
	if err := store.Set("forever", "val", 0); err != nil {
		t.Fatalf("Set forever: %v", err)
	}

	n, err := store.DeleteExpired(context.Background())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}

	// Verify correct rows remain.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM cache").Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 2 {
		t.Errorf("remaining rows = %d, want 2", count)
	}
}

func TestCacheStoreDB_Incr(t *testing.T) {
	db, store := setupCacheStore(t)

	n, err := store.Incr("c", 1, time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Incr miss = (%d, %v), want (1, nil)", n, err)
	}
	n, err = store.Incr("c", 2, time.Minute)
	if err != nil || n != 3 {
		t.Fatalf("Incr accumulate = (%d, %v), want (3, nil)", n, err)
	}

	if err := store.Set("legacy", map[string]any{"count": 4}, time.Minute); err != nil {
		t.Fatalf("Set legacy: %v", err)
	}
	n, err = store.Incr("legacy", 1, time.Minute)
	if err != nil || n != 5 {
		t.Fatalf("Incr legacy = (%d, %v), want (5, nil)", n, err)
	}

	past := time.Now().Add(-time.Minute)
	data, _ := json.Marshal(int64(9))
	if _, err := db.Exec(
		`INSERT INTO cache (key, value, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at`,
		"expired_c", data, past,
	); err != nil {
		t.Fatalf("insert expired: %v", err)
	}
	n, err = store.Incr("expired_c", 1, time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Incr expired = (%d, %v), want (1, nil)", n, err)
	}
}

func TestCacheStoreDB_IncrRejectsInvalidNumericShapes(t *testing.T) {
	db, store := setupCacheStore(t)

	// Fractional JSON number must not cast; treat as miss and start at delta.
	if _, err := db.Exec(
		`INSERT INTO cache (key, value, expires_at) VALUES ($1, '1.5'::jsonb, NULL)`,
		"frac",
	); err != nil {
		t.Fatalf("insert frac: %v", err)
	}
	n, err := store.Incr("frac", 1, time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Incr fractional = (%d, %v), want (1, nil)", n, err)
	}

	// Legacy object with non-integer count must fall back to delta.
	if _, err := db.Exec(
		`INSERT INTO cache (key, value, expires_at) VALUES ($1, '{"count":"abc"}'::jsonb, NULL)`,
		"bad_legacy",
	); err != nil {
		t.Fatalf("insert bad_legacy: %v", err)
	}
	n, err = store.Incr("bad_legacy", 3, time.Minute)
	if err != nil || n != 3 {
		t.Fatalf("Incr invalid legacy count = (%d, %v), want (3, nil)", n, err)
	}

	if _, err := db.Exec(
		`INSERT INTO cache (key, value, expires_at) VALUES ($1, '{"count":2.75}'::jsonb, NULL)`,
		"frac_legacy",
	); err != nil {
		t.Fatalf("insert frac_legacy: %v", err)
	}
	n, err = store.Incr("frac_legacy", 1, time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Incr fractional legacy count = (%d, %v), want (1, nil)", n, err)
	}
}

func TestCacheStoreDB_IncrConcurrent(t *testing.T) {
	_, store := setupCacheStore(t)
	const workers = 40
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.Incr("race", 1, time.Minute); err != nil {
				t.Errorf("Incr: %v", err)
			}
		}()
	}
	wg.Wait()
	var got int64
	hit, err := store.Get("race", &got)
	if err != nil || !hit || got != workers {
		t.Fatalf("Get after concurrent = hit=%v val=%d err=%v, want %d", hit, got, err, workers)
	}
}

func TestCacheStoreDB_CompareAndSubtractBasic(t *testing.T) {
	_, store := setupCacheStore(t)
	if _, err := store.Incr("sub", 5, time.Minute); err != nil {
		t.Fatalf("Incr: %v", err)
	}
	n, err := store.CompareAndSubtract("sub", 3)
	if err != nil || n != 2 {
		t.Fatalf("CompareAndSubtract = (%d, %v), want (2, nil)", n, err)
	}
	n, err = store.CompareAndSubtract("sub", 9)
	if err != nil || n != 2 {
		t.Fatalf("CompareAndSubtract when current < expected = (%d, %v), want (2, nil)", n, err)
	}
	var still int64
	hit, err := store.Get("sub", &still)
	if err != nil || !hit || still != 2 {
		t.Fatalf("Get after no-op = hit=%v val=%d err=%v, want 2", hit, still, err)
	}
	n, err = store.CompareAndSubtract("sub", 2)
	if err != nil || n != 0 {
		t.Fatalf("CompareAndSubtract clear = (%d, %v), want (0, nil)", n, err)
	}
	var got int64
	hit, err = store.Get("sub", &got)
	if err != nil || hit {
		t.Fatalf("Get after clear = hit=%v err=%v, want miss", hit, err)
	}
}

func TestCacheStoreDB_CompareAndSubtractVsIncrConcurrent(t *testing.T) {
	_, store := setupCacheStore(t)
	if _, err := store.Incr("cas", 10, time.Minute); err != nil {
		t.Fatalf("seed: %v", err)
	}
	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.Incr("cas", 1, time.Minute); err != nil {
				t.Errorf("Incr: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := store.CompareAndSubtract("cas", 1); err != nil {
				t.Errorf("CompareAndSubtract: %v", err)
			}
		}()
	}
	wg.Wait()
	var got int64
	hit, err := store.Get("cas", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Net: +workers Incr and -workers Subtract from base 10 → expect 10 if none lost.
	if !hit || got != 10 {
		t.Fatalf("Get after mixed race = hit=%v val=%d, want 10 (lost updates if FOR UPDATE missing)", hit, got)
	}
}

func TestCacheStoreDB_TagInvalidation(t *testing.T) {
	_, store := setupCacheStore(t)
	tagtest.Run(t, store)
}

func TestCacheStoreDB_TagDeleteAfterExpiry(t *testing.T) {
	db, store := setupCacheStore(t)
	past := time.Now().Add(-time.Minute)
	data, err := json.Marshal("stale")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO cache (key, value, expires_at) VALUES ($1, $2, $3)`,
		"expired_tagged", data, past,
	); err != nil {
		t.Fatalf("insert expired: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO cache_tags (tag, key) VALUES ($1, $2)`,
		"exp-tag", "expired_tagged",
	); err != nil {
		t.Fatalf("insert tag: %v", err)
	}

	var got string
	ok, err := store.Get("expired_tagged", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected miss for expired tagged entry")
	}
	n, err := store.DeleteByTag(context.Background(), "exp-tag")
	if err != nil {
		t.Fatalf("DeleteByTag after expiry: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteByTag count = %d, want 1 (the tag row exists regardless of the value's own expiry)", n)
	}
	ok, err = store.Get("expired_tagged", &got)
	if err != nil || ok {
		t.Fatalf("Get after DeleteByTag = hit=%v err=%v, want miss", ok, err)
	}
	var tagCount int
	if err := db.QueryRow(`SELECT count(*) FROM cache_tags WHERE tag = 'exp-tag'`).Scan(&tagCount); err != nil {
		t.Fatalf("count exp-tag: %v", err)
	}
	if tagCount != 0 {
		t.Errorf("cache_tags rows for exp-tag = %d, want 0", tagCount)
	}
}

func TestCacheStoreDB_TagDeleteExpiredSweepsOrphans(t *testing.T) {
	db, store := setupCacheStore(t)
	if err := store.SetWithTags(context.Background(), "alive", "v", time.Hour, "keep"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	if err := store.SetWithTags(context.Background(), "to-delete", "v", 0, "orphan"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	// store.Delete removes the matching cache_tags row itself (see its own
	// doc comment), so it can't be used here — it would leave nothing for
	// DeleteExpired's orphan sweep to actually clean up, and this test
	// would pass without exercising that sweep at all. Deleting the cache
	// row directly via SQL bypasses that cleanup, leaving the "orphan" tag
	// row behind for DeleteExpired to sweep.
	if _, err := db.Exec(`DELETE FROM cache WHERE key = $1`, "to-delete"); err != nil {
		t.Fatalf("delete to-delete: %v", err)
	}

	past := time.Now().Add(-time.Minute)
	data, err := json.Marshal("x")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO cache (key, value, expires_at) VALUES ($1, $2, $3)`,
		"expired", data, past,
	); err != nil {
		t.Fatalf("insert expired: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO cache_tags (tag, key) VALUES ($1, $2)`,
		"expired-tag", "expired",
	); err != nil {
		t.Fatalf("insert expired tag: %v", err)
	}

	n, err := store.DeleteExpired(context.Background())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted cache rows = %d, want 1", n)
	}

	var orphanCount int
	if err := db.QueryRow(`SELECT count(*) FROM cache_tags WHERE tag IN ('orphan', 'expired-tag')`).Scan(&orphanCount); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphanCount != 0 {
		t.Errorf("orphaned tag rows = %d, want 0", orphanCount)
	}
	var keepCount int
	if err := db.QueryRow(`SELECT count(*) FROM cache_tags WHERE tag = 'keep'`).Scan(&keepCount); err != nil {
		t.Fatalf("count keep: %v", err)
	}
	if keepCount != 1 {
		t.Errorf("keep tag rows = %d, want 1", keepCount)
	}
}

func TestCacheStoreDB_TagDeleteByTagCountIncludesOrphans(t *testing.T) {
	db, store := setupCacheStore(t)
	ctx := context.Background()
	if err := store.SetWithTags(ctx, "live", "v", time.Hour, "mix-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO cache_tags (tag, key) VALUES ($1, $2)`, "mix-tag", "already-gone"); err != nil {
		t.Fatalf("insert orphan tag: %v", err)
	}

	n, err := store.DeleteByTag(ctx, "mix-tag")
	if err != nil {
		t.Fatalf("DeleteByTag: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteByTag count = %d, want 2 (snapshot includes the orphaned tag row)", n)
	}

	var got string
	ok, err := store.Get("live", &got)
	if err != nil || ok {
		t.Fatalf("Get live = hit=%v err=%v, want miss", ok, err)
	}
	var tagRows int
	if err := db.QueryRow(`SELECT count(*) FROM cache_tags WHERE tag = 'mix-tag'`).Scan(&tagRows); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tagRows != 0 {
		t.Fatalf("mix-tag rows = %d, want 0", tagRows)
	}
}

// TestCacheStoreDB_TagDeleteByTagOnlyTouchesOwnTagRows pins the code
// review fix: DeleteByTag's doomed CTE must only DELETE cache_tags rows
// for the requested tag itself, not every tag a doomed key happens to
// have. A prior version additionally deleted cache_tags for ALL of a
// doomed key's tags (via a second "tags_gone" CTE keyed on the same
// snapshotted key list) — closing the "other tags left orphaned" gap,
// but reaching past $1's own snapshot into a DIFFERENT tag's row that a
// concurrent SetWithTags could commit (additively, without removing the
// key's existing tag) after this statement's own snapshot but before it
// finished — destroying an association the documented contract says
// must survive. This test doesn't need real concurrency to demonstrate
// the fix: it directly confirms the SQL no longer touches other-tag rows
// for a doomed key at all, regardless of timing.
func TestCacheStoreDB_TagDeleteByTagOnlyTouchesOwnTagRows(t *testing.T) {
	db, store := setupCacheStore(t)
	ctx := context.Background()
	if err := store.SetWithTags(ctx, "racy-key", "v1", time.Hour, "old-tag", "new-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}

	n, err := store.DeleteByTag(ctx, "old-tag")
	if err != nil {
		t.Fatalf("DeleteByTag old-tag: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteByTag old-tag count = %d, want 1", n)
	}

	var newTagRows int
	if err := db.QueryRow(`SELECT count(*) FROM cache_tags WHERE tag = 'new-tag' AND key = 'racy-key'`).Scan(&newTagRows); err != nil {
		t.Fatalf("count new-tag rows: %v", err)
	}
	if newTagRows != 1 {
		t.Fatalf("new-tag rows for racy-key = %d, want 1 (must survive a DeleteByTag for a different tag on the same key)", newTagRows)
	}
}

// TestCacheStoreDB_SetBus_PublishesOnDeleteByTag pins PR-1040's L1
// broadcast: once SetBus is wired, a successful DeleteByTag publishes
// cache.EventInvalidated with the tag, so an in-process L1 cache tier
// can evict its own copy immediately.
func TestCacheStoreDB_SetBus_PublishesOnDeleteByTag(t *testing.T) {
	_, store := setupCacheStore(t)
	ctx := context.Background()
	if err := store.SetWithTags(ctx, "tagged", "v", time.Hour, "bus-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}

	bus := event.NewBus(logger.New("error"))
	store.SetBus(bus)

	captured := newEventCollector()
	bus.OnAsync(cache.EventInvalidated, captured.handle)

	if _, err := store.DeleteByTag(ctx, "bus-tag"); err != nil {
		t.Fatalf("DeleteByTag: %v", err)
	}

	evts := waitForCaptured(t, captured, 1)
	data, ok := evts[0].Data.(cache.InvalidatedData)
	if !ok {
		t.Fatalf("event data type = %T, want cache.InvalidatedData", evts[0].Data)
	}
	if data.Tag != "bus-tag" || data.Prefix != "" {
		t.Errorf("data = %+v, want Tag=bus-tag Prefix=\"\"", data)
	}
}

// TestCacheStoreDB_SetBus_PublishesOnDeleteByPrefix mirrors the above
// for DeleteByPrefix.
func TestCacheStoreDB_SetBus_PublishesOnDeleteByPrefix(t *testing.T) {
	_, store := setupCacheStore(t)
	ctx := context.Background()
	if err := store.Set("product:123:en", "v", time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	bus := event.NewBus(logger.New("error"))
	store.SetBus(bus)

	captured := newEventCollector()
	bus.OnAsync(cache.EventInvalidated, captured.handle)

	if err := store.DeleteByPrefix(ctx, "product:123:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	evts := waitForCaptured(t, captured, 1)
	data, ok := evts[0].Data.(cache.InvalidatedData)
	if !ok {
		t.Fatalf("event data type = %T, want cache.InvalidatedData", evts[0].Data)
	}
	if data.Prefix != "product:123:" || data.Tag != "" {
		t.Errorf("data = %+v, want Prefix=product:123: Tag=\"\"", data)
	}
}

// TestCacheStoreDB_NoBusNoEvent pins that DeleteByTag/DeleteByPrefix work
// exactly as before when SetBus was never called — the default for every
// existing caller/test that doesn't need the broadcast.
func TestCacheStoreDB_NoBusNoEvent(t *testing.T) {
	_, store := setupCacheStore(t)
	ctx := context.Background()
	if err := store.SetWithTags(ctx, "tagged", "v", time.Hour, "no-bus-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	if _, err := store.DeleteByTag(ctx, "no-bus-tag"); err != nil {
		t.Fatalf("DeleteByTag: %v", err)
	}
	if err := store.DeleteByPrefix(ctx, "anything:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
}

// eventCollector records events from an async bus handler under a mutex —
// bus.OnAsync dispatches in its own goroutine (see Bus.Publish/PublishAsync's
// own doc comments), so a test observing that side effect from its own
// goroutine (e.g. polling in waitForCaptured) must synchronize the shared
// slice, not read/write it directly as a bare `var captured []event.Event`.
type eventCollector struct {
	mu     sync.Mutex
	events []event.Event
}

func newEventCollector() *eventCollector {
	return &eventCollector{}
}

func (c *eventCollector) handle(_ context.Context, evt event.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, evt)
	return nil
}

func (c *eventCollector) snapshot() []event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]event.Event(nil), c.events...)
}

// waitForCaptured polls captured for up to one second and returns its
// contents once at least want events have arrived, failing the test if
// that deadline passes first.
func waitForCaptured(t *testing.T, captured *eventCollector, want int) []event.Event {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if evts := captured.snapshot(); len(evts) >= want {
			return evts
		}
		time.Sleep(time.Millisecond)
	}
	evts := captured.snapshot()
	if len(evts) < want {
		t.Fatalf("captured %d events, want at least %d", len(evts), want)
	}
	return evts
}
