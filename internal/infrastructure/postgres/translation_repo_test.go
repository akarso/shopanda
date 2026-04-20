package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/domain/translation"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
)

func mustNewTranslation(t *testing.T, key, language, value string) *translation.Translation {
	t.Helper()
	tr, err := translation.NewTranslation(key, language, value)
	if err != nil {
		t.Fatalf("NewTranslation: %v", err)
	}
	return &tr
}

func TestTranslationRepo_NilDB(t *testing.T) {
	_, err := postgres.NewTranslationRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestTranslationRepo_UpsertAndFind(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM translations") })

	repo, err := postgres.NewTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewTranslationRepo: %v", err)
	}
	ctx := context.Background()

	tr := mustNewTranslation(t, "greeting", "en", "Hello")
	if err := repo.Upsert(ctx, tr); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.FindByKeyAndLanguage(ctx, "greeting", "en")
	if err != nil {
		t.Fatalf("FindByKeyAndLanguage: %v", err)
	}
	if got == nil {
		t.Fatal("FindByKeyAndLanguage returned nil")
	}
	if got.Key != "greeting" {
		t.Errorf("Key: got %q, want %q", got.Key, "greeting")
	}
	if got.Language != "en" {
		t.Errorf("Language: got %q, want %q", got.Language, "en")
	}
	if got.Value != "Hello" {
		t.Errorf("Value: got %q, want %q", got.Value, "Hello")
	}
}

func TestTranslationRepo_Upsert_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM translations") })

	repo, err := postgres.NewTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewTranslationRepo: %v", err)
	}
	ctx := context.Background()

	tr := mustNewTranslation(t, "farewell", "de", "Tschüss")
	if err := repo.Upsert(ctx, tr); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}

	tr2 := mustNewTranslation(t, "farewell", "de", "Auf Wiedersehen")
	if err := repo.Upsert(ctx, tr2); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	got, err := repo.FindByKeyAndLanguage(ctx, "farewell", "de")
	if err != nil {
		t.Fatalf("FindByKeyAndLanguage: %v", err)
	}
	if got == nil {
		t.Fatal("FindByKeyAndLanguage returned nil")
	}
	if got.Value != "Auf Wiedersehen" {
		t.Errorf("Value: got %q, want %q", got.Value, "Auf Wiedersehen")
	}
}

func TestTranslationRepo_ListByLanguage(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM translations") })

	repo, err := postgres.NewTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewTranslationRepo: %v", err)
	}
	ctx := context.Background()

	for _, kv := range []struct{ key, lang, val string }{
		{"hello", "fr", "Bonjour"},
		{"bye", "fr", "Au revoir"},
		{"hello", "en", "Hello"},
	} {
		tr := mustNewTranslation(t, kv.key, kv.lang, kv.val)
		if err := repo.Upsert(ctx, tr); err != nil {
			t.Fatalf("Upsert %q/%q: %v", kv.key, kv.lang, err)
		}
	}

	list, err := repo.ListByLanguage(ctx, "fr")
	if err != nil {
		t.Fatalf("ListByLanguage: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListByLanguage: got %d, want 2", len(list))
	}
}

func TestTranslationRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM translations") })

	repo, err := postgres.NewTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewTranslationRepo: %v", err)
	}
	ctx := context.Background()

	tr := mustNewTranslation(t, "temp", "en", "Temporary")
	if err := repo.Upsert(ctx, tr); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := repo.Delete(ctx, "temp", "en"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByKeyAndLanguage(ctx, "temp", "en")
	if err != nil {
		t.Fatalf("FindByKeyAndLanguage: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestTranslationRepo_Delete_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewTranslationRepo: %v", err)
	}

	err = repo.Delete(context.Background(), "nonexistent", "en")
	if err == nil {
		t.Fatal("expected error deleting non-existent translation")
	}
	if !errors.Is(err, translation.ErrNotFound) {
		t.Errorf("expected translation.ErrNotFound, got: %v", err)
	}
}

func TestTranslationRepo_FindByKeyAndLanguage_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewTranslationRepo: %v", err)
	}

	got, err := repo.FindByKeyAndLanguage(context.Background(), "missing", "en")
	if err != nil {
		t.Fatalf("FindByKeyAndLanguage: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent translation")
	}
}
