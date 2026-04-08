package postgres_test

import (
	"database/sql"
	"encoding/json"
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
	return db, postgres.NewCacheStore(db)
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

	store := postgres.NewCacheStore(db)
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

	n, err := store.DeleteExpired()
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
