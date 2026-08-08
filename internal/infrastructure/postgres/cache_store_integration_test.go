package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

// setupCacheStore opens a test DB, runs migrations, and registers cache cleanup.
func setupCacheStore(t *testing.T) (*sql.DB, *postgres.CacheStore) {
	t.Helper()
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM cache") })
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
