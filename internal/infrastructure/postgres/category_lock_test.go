package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestNewAdvisoryLock_NilDB(t *testing.T) {
	if _, err := postgres.NewAdvisoryLock(nil); err == nil {
		t.Fatal("expected error for nil *sql.DB")
	}
}

// secondTestDB opens an independent *sql.DB pool against the same test
// database — a distinct connection pool, deliberately not sharing
// anything with testDB(t)'s own pool, standing in for a second API server
// instance connecting to the same Postgres database. This is the whole
// point of this test file: a plain in-process sync.Mutex cannot be
// observed from a different pool/process, but a Postgres advisory lock
// must be.
func secondTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open second db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping second db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestAdvisoryLock_Lock_SerializesAcrossConnections is the core proof
// this lock actually works across separate instances (not just goroutines
// sharing one process's memory): two AdvisoryLocks, each backed by its
// own independent *sql.DB connection pool, both locking the same key.
// The second Lock call must block until the first's unlock releases it —
// exactly the guarantee IndexUpdateSubscriber's category handlers depend
// on to stay correct when Update and Delete for the same category are
// handled by two different API server instances.
func TestAdvisoryLock_Lock_SerializesAcrossConnections(t *testing.T) {
	dbA := testDB(t)
	dbB := secondTestDB(t)

	lockA, err := postgres.NewAdvisoryLock(dbA)
	if err != nil {
		t.Fatalf("NewAdvisoryLock(A): %v", err)
	}
	lockB, err := postgres.NewAdvisoryLock(dbB)
	if err != nil {
		t.Fatalf("NewAdvisoryLock(B): %v", err)
	}

	key := "cat-" + id.New()
	ctx := context.Background()

	unlockA, err := lockA.Lock(ctx, key)
	if err != nil {
		t.Fatalf("lockA.Lock: %v", err)
	}

	bAcquired := make(chan struct{})
	unlockBCh := make(chan func() error, 1)
	go func() {
		unlockB, err := lockB.Lock(ctx, key)
		if err != nil {
			t.Errorf("lockB.Lock: %v", err)
			close(bAcquired)
			return
		}
		unlockBCh <- unlockB
		close(bAcquired)
	}()

	select {
	case <-bAcquired:
		t.Fatal("lockB.Lock acquired the same key while lockA still held it — the lock is not actually serializing across connections")
	case <-time.After(200 * time.Millisecond):
		// Expected: still blocked.
	}

	if err := unlockA(); err != nil {
		t.Fatalf("unlockA: %v", err)
	}

	select {
	case <-bAcquired:
		unlockB := <-unlockBCh
		if unlockB != nil {
			if err := unlockB(); err != nil {
				t.Fatalf("unlockB: %v", err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lockB.Lock did not acquire the key after lockA released it")
	}
}

// TestAdvisoryLock_Lock_DifferentKeysDoNotBlock is the sanity counterpart:
// unrelated category IDs must not serialize against each other, or every
// category write in the system would queue up behind an unrelated one.
func TestAdvisoryLock_Lock_DifferentKeysDoNotBlock(t *testing.T) {
	dbA := testDB(t)
	dbB := secondTestDB(t)

	lockA, err := postgres.NewAdvisoryLock(dbA)
	if err != nil {
		t.Fatalf("NewAdvisoryLock(A): %v", err)
	}
	lockB, err := postgres.NewAdvisoryLock(dbB)
	if err != nil {
		t.Fatalf("NewAdvisoryLock(B): %v", err)
	}

	ctx := context.Background()
	unlockA, err := lockA.Lock(ctx, "cat-"+id.New())
	if err != nil {
		t.Fatalf("lockA.Lock: %v", err)
	}
	defer unlockA()

	done := make(chan error, 1)
	go func() {
		unlockB, err := lockB.Lock(ctx, "cat-"+id.New())
		if err == nil {
			defer unlockB()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("lockB.Lock (different key): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lockB.Lock on a different key blocked — different categories must not serialize against each other")
	}
}
