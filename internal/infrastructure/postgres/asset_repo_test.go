package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func mustNewAsset(t *testing.T, filename string) *media.Asset {
	t.Helper()
	a, err := media.NewAsset(id.New(), "/uploads/"+filename, filename, "image/png", 1024)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	return &a
}

func TestAssetRepo_NilDB(t *testing.T) {
	_, err := postgres.NewAssetRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestAssetRepo_SaveAndFindByID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM assets")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM assets") })

	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	ctx := context.Background()

	a := mustNewAsset(t, "logo.png")
	if err := repo.Save(ctx, a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.ID != a.ID {
		t.Errorf("ID: got %q, want %q", got.ID, a.ID)
	}
	if got.Path != a.Path {
		t.Errorf("Path: got %q, want %q", got.Path, a.Path)
	}
	if got.Filename != "logo.png" {
		t.Errorf("Filename: got %q, want %q", got.Filename, "logo.png")
	}
	if got.MimeType != "image/png" {
		t.Errorf("MimeType: got %q, want %q", got.MimeType, "image/png")
	}
	if got.Size != 1024 {
		t.Errorf("Size: got %d, want 1024", got.Size)
	}
}

func TestAssetRepo_FindByID_NotFound(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}

	got, err := repo.FindByID(context.Background(), id.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent asset")
	}
}

func TestAssetRepo_Save_WithMeta(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM assets")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM assets") })

	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	ctx := context.Background()

	a := mustNewAsset(t, "banner.jpg")
	a.Meta = map[string]interface{}{
		"width":  float64(1920),
		"height": float64(1080),
	}
	a.Thumbnails = map[string]string{
		"small":  "/uploads/banner_sm.jpg",
		"medium": "/uploads/banner_md.jpg",
	}

	if err := repo.Save(ctx, a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}

	if w, ok := got.Meta["width"].(float64); !ok || w != 1920 {
		t.Errorf("Meta[width]: got %v, want 1920", got.Meta["width"])
	}
	if h, ok := got.Meta["height"].(float64); !ok || h != 1080 {
		t.Errorf("Meta[height]: got %v, want 1080", got.Meta["height"])
	}
	if got.Thumbnails["small"] != "/uploads/banner_sm.jpg" {
		t.Errorf("Thumbnails[small]: got %q, want %q", got.Thumbnails["small"], "/uploads/banner_sm.jpg")
	}
	if got.Thumbnails["medium"] != "/uploads/banner_md.jpg" {
		t.Errorf("Thumbnails[medium]: got %q, want %q", got.Thumbnails["medium"], "/uploads/banner_md.jpg")
	}
}

func TestAssetRepo_List(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM assets")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM assets") })

	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	ctx := context.Background()

	a1 := mustNewAsset(t, "logo-1.png")
	a2 := mustNewAsset(t, "logo-2.png")
	if err := repo.Save(ctx, a1); err != nil {
		t.Fatalf("Save a1: %v", err)
	}
	if err := repo.Save(ctx, a2); err != nil {
		t.Fatalf("Save a2: %v", err)
	}

	items, err := repo.List(ctx, 0, 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
}

func TestAssetRepo_Delete(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM assets")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM assets") })

	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	ctx := context.Background()

	a := mustNewAsset(t, "delete.png")
	if err := repo.Save(ctx, a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := repo.Delete(ctx, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := repo.FindByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}
