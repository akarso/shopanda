package postgres_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
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
	now := time.Now().UTC()
	a1.CreatedAt = now.Add(-time.Hour)
	a2.CreatedAt = now
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
	if items[0].ID != a2.ID {
		t.Fatalf("items[0].ID = %q, want %q", items[0].ID, a2.ID)
	}
	if items[1].ID != a1.ID {
		t.Fatalf("items[1].ID = %q, want %q", items[1].ID, a1.ID)
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

func TestAssetRepo_Delete_EmptyID(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}

	err = repo.Delete(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestAssetRepo_LockForUpdate_RequiresTx(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://invalid/invalid")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	err = repo.LockForUpdate(context.Background(), id.New())
	if err == nil || !strings.Contains(err.Error(), "transaction required") {
		t.Fatalf("LockForUpdate = %v, want transaction required", err)
	}
}

func TestAssetRepo_LockForUpdate(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)
	mustExec(t, db, "DELETE FROM assets")
	t.Cleanup(func() { mustExec(t, db, "DELETE FROM assets") })

	repo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	ctx := context.Background()
	a := mustNewAsset(t, "lock.png")
	if err := repo.Save(ctx, a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	bound, ok := repo.WithTx(tx).(*postgres.AssetRepo)
	if !ok {
		t.Fatalf("WithTx returned %T", repo.WithTx(tx))
	}
	if err := bound.LockForUpdate(ctx, a.ID); err != nil {
		t.Fatalf("LockForUpdate: %v", err)
	}
	if err := bound.LockForUpdate(ctx, id.New()); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("missing lock = %v, want not found", err)
	}

	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx second: %v", err)
	}
	defer tx2.Rollback()
	if _, err := tx2.ExecContext(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	start := time.Now()
	err = repo.WithTx(tx2).(*postgres.AssetRepo).LockForUpdate(ctx, a.ID)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("second lock acquired while first transaction holds FOR UPDATE")
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("lock returned in %s, want a wait then failure: %v", elapsed, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "lock") {
		t.Fatalf("LockForUpdate = %v, want lock timeout", err)
	}
}
