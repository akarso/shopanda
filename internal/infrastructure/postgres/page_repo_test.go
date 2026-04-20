package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewPage(t *testing.T, slug, title string) *cms.Page {
	t.Helper()
	p, err := cms.NewPage(id.New(), slug, title, "body of "+title)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	return p
}

func TestPageRepo_NilDB(t *testing.T) {
	_, err := postgres.NewPageRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestPageRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPage(t, "about-us", "About Us")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.Slug() != "about-us" {
		t.Errorf("Slug: got %q, want %q", got.Slug(), "about-us")
	}
	if got.Title() != "About Us" {
		t.Errorf("Title: got %q, want %q", got.Title(), "About Us")
	}
	if !got.IsActive() {
		t.Error("expected IsActive=true")
	}
}

func TestPageRepo_FindBySlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPage(t, "privacy", "Privacy Policy")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindBySlug(ctx, "privacy")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if got == nil {
		t.Fatal("FindBySlug returned nil")
	}
	if got.ID() != p.ID() {
		t.Errorf("ID: got %q, want %q", got.ID(), p.ID())
	}
}

func TestPageRepo_FindActiveBySlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	active := mustNewPage(t, "terms", "Terms")
	if err := repo.Create(ctx, active); err != nil {
		t.Fatalf("Create active: %v", err)
	}

	inactive := mustNewPage(t, "draft-page", "Draft")
	inactive.SetActive(false)
	if err := repo.Create(ctx, inactive); err != nil {
		t.Fatalf("Create inactive: %v", err)
	}

	got, err := repo.FindActiveBySlug(ctx, "terms")
	if err != nil {
		t.Fatalf("FindActiveBySlug: %v", err)
	}
	if got == nil {
		t.Fatal("FindActiveBySlug returned nil for active page")
	}

	got2, err := repo.FindActiveBySlug(ctx, "draft-page")
	if err != nil {
		t.Fatalf("FindActiveBySlug draft: %v", err)
	}
	if got2 != nil {
		t.Error("expected nil for inactive page")
	}
}

func TestPageRepo_List(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	for i, slug := range []string{"page-a", "page-b", "page-c"} {
		p := mustNewPage(t, slug, "Page "+string(rune('A'+i)))
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %q: %v", slug, err)
		}
	}

	pages, err := repo.List(ctx, 0, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("List: got %d, want 3", len(pages))
	}
}

func TestPageRepo_List_Pagination(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	for i, slug := range []string{"pg-1", "pg-2", "pg-3"} {
		p := mustNewPage(t, slug, "Pg "+string(rune('1'+i)))
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	pages, err := repo.List(ctx, 0, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pages) != 2 {
		t.Errorf("List limit=2: got %d, want 2", len(pages))
	}

	pages2, err := repo.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List offset=2: %v", err)
	}
	if len(pages2) != 1 {
		t.Errorf("List offset=2: got %d, want 1", len(pages2))
	}
}

func TestPageRepo_List_InvalidArgs(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	if _, err := repo.List(ctx, -1, 10); err == nil {
		t.Error("expected error for negative offset")
	}
	if _, err := repo.List(ctx, 0, 0); err == nil {
		t.Error("expected error for zero limit")
	}
}

func TestPageRepo_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPage(t, "old-slug", "Old Title")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	p.SetTitle("New Title")
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Title() != "New Title" {
		t.Errorf("Title: got %q, want %q", got.Title(), "New Title")
	}
}

func TestPageRepo_Update_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}

	p := mustNewPage(t, "ghost", "Ghost")
	err = repo.Update(context.Background(), p)
	if err == nil {
		t.Fatal("expected error updating non-existent page")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestPageRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	p := mustNewPage(t, "to-delete", "Delete Me")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, p.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByID(ctx, p.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestPageRepo_Delete_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}

	err = repo.Delete(context.Background(), id.New())
	if err == nil {
		t.Fatal("expected error deleting non-existent page")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestPageRepo_Create_DuplicateSlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM pages")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM pages") })

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}
	ctx := context.Background()

	p1 := mustNewPage(t, "dup", "First")
	if err := repo.Create(ctx, p1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	p2 := mustNewPage(t, "dup", "Second")
	err = repo.Create(ctx, p2)
	if err == nil {
		t.Fatal("expected conflict for duplicate slug")
	}
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected Conflict, got: %v", err)
	}
}

func TestPageRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}

	_, err = repo.FindByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestPageRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}

	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent page")
	}
}

func TestPageRepo_Create_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewPageRepo(db)
	if err != nil {
		t.Fatalf("NewPageRepo: %v", err)
	}

	if err := repo.Create(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil page")
	}
}
