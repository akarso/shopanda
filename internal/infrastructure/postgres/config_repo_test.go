package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func setupConfigRepo(t *testing.T) *postgres.ConfigRepo {
	t.Helper()
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DELETE FROM config"); err != nil {
			t.Errorf("cleanup DELETE FROM config: %v", err)
		}
	})
	return postgres.NewConfigRepo(db)
}

func TestConfigRepo_SetAndGet(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Set(ctx, "tax.rate", 0.20); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := repo.Get(ctx, "tax.rate")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// JSON numbers decode as float64.
	f, ok := val.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", val)
	}
	if f != 0.20 {
		t.Errorf("value = %v, want 0.2", f)
	}
}

func TestConfigRepo_GetMiss(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	val, err := repo.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

func TestConfigRepo_Upsert(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Set(ctx, "k1", "first"); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := repo.Set(ctx, "k1", "second"); err != nil {
		t.Fatalf("Set second: %v", err)
	}

	val, err := repo.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "second" {
		t.Errorf("value = %v, want %q", val, "second")
	}
}

func TestConfigRepo_Delete(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Set(ctx, "k1", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := repo.Delete(ctx, "k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	val, err := repo.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil after delete, got %v", val)
	}
}

func TestConfigRepo_DeleteMissing(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Delete(ctx, "nope"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestConfigRepo_All(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Set(ctx, "b.key", "bval"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := repo.Set(ctx, "a.key", "aval"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entries, err := repo.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// Ordered by key.
	if entries[0].Key != "a.key" {
		t.Errorf("first key = %q, want %q", entries[0].Key, "a.key")
	}
	if entries[1].Key != "b.key" {
		t.Errorf("second key = %q, want %q", entries[1].Key, "b.key")
	}
}

func TestConfigRepo_AllEmpty(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	entries, err := repo.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

func TestConfigRepo_SetNilValue(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Set(ctx, "k", nil); err == nil {
		t.Fatal("expected error for nil value")
	}
}

func TestConfigRepo_ComplexValue(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	complex := map[string]interface{}{
		"enabled": true,
		"rate":    9.99,
		"tags":    []interface{}{"a", "b"},
	}
	if err := repo.Set(ctx, "plugin.settings", complex); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := repo.Get(ctx, "plugin.settings")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if m["enabled"] != true {
		t.Errorf("enabled = %v", m["enabled"])
	}
}
