package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewRewrite(t *testing.T, path, typ string) *routing.URLRewrite {
	t.Helper()
	rw, err := routing.NewURLRewrite(path, typ, id.New())
	if err != nil {
		t.Fatalf("NewURLRewrite: %v", err)
	}
	return rw
}

func TestRewriteRepo_NilDB(t *testing.T) {
	_, err := postgres.NewRewriteRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestRewriteRepo_SaveAndFindByPath(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM url_rewrites")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM url_rewrites") })

	repo, err := postgres.NewRewriteRepo(db)
	if err != nil {
		t.Fatalf("NewRewriteRepo: %v", err)
	}
	ctx := context.Background()

	rw := mustNewRewrite(t, "/products/shoes", "product")
	if err := repo.Save(ctx, rw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByPath(ctx, "/products/shoes")
	if err != nil {
		t.Fatalf("FindByPath: %v", err)
	}
	if got == nil {
		t.Fatal("FindByPath returned nil")
	}
	if got.Path() != "/products/shoes" {
		t.Errorf("Path: got %q, want %q", got.Path(), "/products/shoes")
	}
	if got.Type() != "product" {
		t.Errorf("Type: got %q, want %q", got.Type(), "product")
	}
	if got.EntityID() != rw.EntityID() {
		t.Errorf("EntityID: got %q, want %q", got.EntityID(), rw.EntityID())
	}
}

func TestRewriteRepo_Save_Upsert(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM url_rewrites")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM url_rewrites") })

	repo, err := postgres.NewRewriteRepo(db)
	if err != nil {
		t.Fatalf("NewRewriteRepo: %v", err)
	}
	ctx := context.Background()

	rw1 := mustNewRewrite(t, "/blog/hello", "page")
	if err := repo.Save(ctx, rw1); err != nil {
		t.Fatalf("Save first: %v", err)
	}

	rw2 := mustNewRewrite(t, "/blog/hello", "category")
	if err := repo.Save(ctx, rw2); err != nil {
		t.Fatalf("Save upsert: %v", err)
	}

	got, err := repo.FindByPath(ctx, "/blog/hello")
	if err != nil {
		t.Fatalf("FindByPath: %v", err)
	}
	if got == nil {
		t.Fatal("FindByPath returned nil after upsert")
	}
	if got.Type() != "category" {
		t.Errorf("Type after upsert: got %q, want %q", got.Type(), "category")
	}
	if got.EntityID() != rw2.EntityID() {
		t.Errorf("EntityID after upsert: got %q, want %q", got.EntityID(), rw2.EntityID())
	}
}

func TestRewriteRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM url_rewrites")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM url_rewrites") })

	repo, err := postgres.NewRewriteRepo(db)
	if err != nil {
		t.Fatalf("NewRewriteRepo: %v", err)
	}
	ctx := context.Background()

	rw := mustNewRewrite(t, "/to-delete", "page")
	if err := repo.Save(ctx, rw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := repo.Delete(ctx, "/to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.FindByPath(ctx, "/to-delete")
	if err != nil {
		t.Fatalf("FindByPath: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestRewriteRepo_FindByPath_EmptyPath(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewRewriteRepo(db)
	if err != nil {
		t.Fatalf("NewRewriteRepo: %v", err)
	}

	_, err = repo.FindByPath(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestRewriteRepo_FindByPath_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewRewriteRepo(db)
	if err != nil {
		t.Fatalf("NewRewriteRepo: %v", err)
	}

	got, err := repo.FindByPath(context.Background(), "/no-such-path-"+id.New())
	if err != nil {
		t.Fatalf("FindByPath: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent path")
	}
}

func TestRewriteRepo_Save_Nil(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewRewriteRepo(db)
	if err != nil {
		t.Fatalf("NewRewriteRepo: %v", err)
	}

	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil rewrite")
	}
}
