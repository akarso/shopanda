package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/translation"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewContentTranslation(t *testing.T, entityID, language, field, value string) *translation.ContentTranslation {
	t.Helper()
	ct, err := translation.NewContentTranslation(entityID, language, field, value)
	if err != nil {
		t.Fatalf("NewContentTranslation: %v", err)
	}
	return &ct
}

func TestContentTranslationRepo_NilDB(t *testing.T) {
	_, err := postgres.NewContentTranslationRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestContentTranslationRepo_UpsertAndFindFieldValue(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM content_translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM content_translations") })

	repo, err := postgres.NewContentTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewContentTranslationRepo: %v", err)
	}
	ctx := context.Background()

	entityID := id.New()
	ct := mustNewContentTranslation(t, entityID, "en", "title", "English Title")
	if err := repo.Upsert(ctx, ct); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.FindFieldValue(ctx, entityID, "en", "title")
	if err != nil {
		t.Fatalf("FindFieldValue: %v", err)
	}
	if got == nil {
		t.Fatal("FindFieldValue returned nil")
	}
	if got.Value != "English Title" {
		t.Errorf("Value: got %q, want %q", got.Value, "English Title")
	}
}

func TestContentTranslationRepo_Upsert_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM content_translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM content_translations") })

	repo, err := postgres.NewContentTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewContentTranslationRepo: %v", err)
	}
	ctx := context.Background()

	entityID := id.New()
	ct := mustNewContentTranslation(t, entityID, "de", "description", "Alt")
	if err := repo.Upsert(ctx, ct); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}

	ct2 := mustNewContentTranslation(t, entityID, "de", "description", "Neu")
	if err := repo.Upsert(ctx, ct2); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}

	got, err := repo.FindFieldValue(ctx, entityID, "de", "description")
	if err != nil {
		t.Fatalf("FindFieldValue: %v", err)
	}
	if got == nil {
		t.Fatal("FindFieldValue returned nil after upsert update")
	}
	if got.Value != "Neu" {
		t.Errorf("Value: got %q, want %q", got.Value, "Neu")
	}
}

func TestContentTranslationRepo_FindByEntityAndLanguage(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM content_translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM content_translations") })

	repo, err := postgres.NewContentTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewContentTranslationRepo: %v", err)
	}
	ctx := context.Background()

	entityID := id.New()
	for _, f := range []struct{ field, value string }{
		{"title", "Titre"},
		{"description", "Description"},
	} {
		ct := mustNewContentTranslation(t, entityID, "fr", f.field, f.value)
		if err := repo.Upsert(ctx, ct); err != nil {
			t.Fatalf("Upsert %q: %v", f.field, err)
		}
	}

	list, err := repo.FindByEntityAndLanguage(ctx, entityID, "fr")
	if err != nil {
		t.Fatalf("FindByEntityAndLanguage: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("FindByEntityAndLanguage: got %d, want 2", len(list))
	}
}

func TestContentTranslationRepo_FindByEntityAndLanguage_Empty(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewContentTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewContentTranslationRepo: %v", err)
	}

	list, err := repo.FindByEntityAndLanguage(context.Background(), id.New(), "en")
	if err != nil {
		t.Fatalf("FindByEntityAndLanguage: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(list) != 0 {
		t.Errorf("expected empty slice, got %d items", len(list))
	}
}

func TestContentTranslationRepo_FindFieldValue_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewContentTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewContentTranslationRepo: %v", err)
	}

	got, err := repo.FindFieldValue(context.Background(), id.New(), "en", "title")
	if err != nil {
		t.Fatalf("FindFieldValue: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent content translation")
	}
}

func TestContentTranslationRepo_DeleteByEntity(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM content_translations")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM content_translations") })

	repo, err := postgres.NewContentTranslationRepo(db)
	if err != nil {
		t.Fatalf("NewContentTranslationRepo: %v", err)
	}
	ctx := context.Background()

	entityID := id.New()
	ct := mustNewContentTranslation(t, entityID, "en", "title", "Gone")
	if err := repo.Upsert(ctx, ct); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := repo.DeleteByEntity(ctx, entityID); err != nil {
		t.Fatalf("DeleteByEntity: %v", err)
	}

	list, err := repo.FindByEntityAndLanguage(ctx, entityID, "en")
	if err != nil {
		t.Fatalf("FindByEntityAndLanguage: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty after delete, got %d", len(list))
	}
}
