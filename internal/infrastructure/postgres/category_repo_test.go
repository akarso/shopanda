package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewCategory(t *testing.T, name, slug string) catalog.Category {
	t.Helper()
	c, err := catalog.NewCategory(id.New(), name, slug)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	return c
}

func TestCategoryRepo_NilDB(t *testing.T) {
	_, err := postgres.NewCategoryRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestCategoryRepo_CreateAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM categories")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM categories") })

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCategory(t, "Electronics", "electronics")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.Name != "Electronics" {
		t.Errorf("Name: got %q, want %q", got.Name, "Electronics")
	}
	if got.Slug != "electronics" {
		t.Errorf("Slug: got %q, want %q", got.Slug, "electronics")
	}
}

func TestCategoryRepo_FindBySlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM categories")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM categories") })

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCategory(t, "Books", "books")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindBySlug(ctx, "books")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if got == nil {
		t.Fatal("FindBySlug returned nil")
	}
	if got.ID != c.ID {
		t.Errorf("ID: got %q, want %q", got.ID, c.ID)
	}
}

func TestCategoryRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}

	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent category")
	}
}

func TestCategoryRepo_FindByID_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}

	_, err = repo.FindByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestCategoryRepo_FindBySlug_EmptySlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}

	_, err = repo.FindBySlug(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestCategoryRepo_FindByParentID_Roots(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM categories")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM categories") })

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	ctx := context.Background()

	c1 := mustNewCategory(t, "Root A", "root-a")
	c2 := mustNewCategory(t, "Root B", "root-b")
	for _, c := range []*catalog.Category{&c1, &c2} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create %q: %v", c.Name, err)
		}
	}

	roots, err := repo.FindByParentID(ctx, nil)
	if err != nil {
		t.Fatalf("FindByParentID(nil): %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("roots: got %d, want 2", len(roots))
	}
}

func TestCategoryRepo_FindByParentID_Children(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM categories")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM categories") })

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	ctx := context.Background()

	parent := mustNewCategory(t, "Parent", "parent")
	if err := repo.Create(ctx, &parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	child := mustNewCategory(t, "Child", "child")
	child.ParentID = &parent.ID
	if err := repo.Create(ctx, &child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	children, err := repo.FindByParentID(ctx, &parent.ID)
	if err != nil {
		t.Fatalf("FindByParentID: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("children: got %d, want 1", len(children))
	}
	if children[0].ID != child.ID {
		t.Errorf("child ID: got %q, want %q", children[0].ID, child.ID)
	}
}

func TestCategoryRepo_Update(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM categories")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM categories") })

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	ctx := context.Background()

	c := mustNewCategory(t, "Before", "before")
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	c.Name = "After"
	c.Slug = "after"
	if err := repo.Update(ctx, &c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "After" {
		t.Errorf("Name: got %q, want %q", got.Name, "After")
	}
}

func TestCategoryRepo_Update_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}

	c := mustNewCategory(t, "Ghost", "ghost")
	err = repo.Update(context.Background(), &c)
	if err == nil {
		t.Fatal("expected error updating non-existent category")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected NotFound, got: %v", err)
	}
}

func TestCategoryRepo_Create_DuplicateSlug(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM categories")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM categories") })

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	ctx := context.Background()

	c1 := mustNewCategory(t, "First", "dup-slug")
	if err := repo.Create(ctx, &c1); err != nil {
		t.Fatalf("Create: %v", err)
	}

	c2 := mustNewCategory(t, "Second", "dup-slug")
	err = repo.Create(ctx, &c2)
	if err == nil {
		t.Fatal("expected conflict for duplicate slug")
	}
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Errorf("expected Conflict, got: %v", err)
	}
}

func TestCategoryRepo_FindAll(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM categories")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM categories") })

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}
	ctx := context.Background()

	c1 := mustNewCategory(t, "Alpha", "alpha")
	c2 := mustNewCategory(t, "Beta", "beta")
	for _, c := range []*catalog.Category{&c1, &c2} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create %q: %v", c.Name, err)
		}
	}

	all, err := repo.FindAll(ctx)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("FindAll: got %d, want 2", len(all))
	}
}

func TestCategoryRepo_Create_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		t.Fatalf("NewCategoryRepo: %v", err)
	}

	if err := repo.Create(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil category")
	}
}
