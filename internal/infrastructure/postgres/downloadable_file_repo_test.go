package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestDownloadableFileRepo_NilDB(t *testing.T) {
	_, err := postgres.NewDownloadableFileRepo(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestDownloadableFileRepo_CRUD(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	fileRepo, err := postgres.NewDownloadableFileRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadableFileRepo: %v", err)
	}
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}

	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "Ebook", "ebook-dl")
	p.Type = catalog.TypeDownloadable
	p.Status = catalog.StatusDraft
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	asset, err := media.NewAsset(id.New(), "files/ebook.pdf", "ebook.pdf", "application/pdf", 1024)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}

	f, err := catalog.NewDownloadableFile(p.ID, asset.ID)
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	f.Position = 1
	f.Label = "PDF"
	if err := fileRepo.Upsert(ctx, f); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	n, err := fileRepo.CountByProductID(ctx, p.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountByProductID = %d, %v", n, err)
	}
	an, err := fileRepo.CountByAssetID(ctx, asset.ID)
	if err != nil || an != 1 {
		t.Fatalf("CountByAssetID = %d, %v", an, err)
	}

	rows, err := fileRepo.ListByProductID(ctx, p.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].AssetID != asset.ID || rows[0].Label != "PDF" || rows[0].Position != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].CreatedAt.IsZero() || time.Since(rows[0].CreatedAt) > time.Minute {
		t.Fatalf("CreatedAt = %v", rows[0].CreatedAt)
	}

	f.Label = "Updated"
	f.Position = 2
	if err := fileRepo.Upsert(ctx, f); err != nil {
		t.Fatalf("Upsert update: %v", err)
	}
	rows, err = fileRepo.ListByProductID(ctx, p.ID)
	if err != nil || len(rows) != 1 || rows[0].Label != "Updated" || rows[0].Position != 2 {
		t.Fatalf("after update = %+v err=%v", rows, err)
	}

	if err := fileRepo.Remove(ctx, p.ID, asset.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	n, err = fileRepo.CountByProductID(ctx, p.ID)
	if err != nil || n != 0 {
		t.Fatalf("count after remove = %d, %v", n, err)
	}
	err = fileRepo.Remove(ctx, p.ID, asset.ID)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("second Remove = %v, want NotFound", err)
	}
}

func TestDownloadableFileRepo_MissingAsset(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	fileRepo, err := postgres.NewDownloadableFileRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadableFileRepo: %v", err)
	}
	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "Ebook", "ebook-missing-asset")
	p.Type = catalog.TypeDownloadable
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	f, err := catalog.NewDownloadableFile(p.ID, "missing-asset")
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	err = fileRepo.Upsert(ctx, f)
	if !apperror.Is(err, apperror.CodeNotFound) || !strings.Contains(err.Error(), "asset") {
		t.Fatalf("Upsert = %v, want asset not found", err)
	}
}

func TestDownloadableFileRepo_ProductDeleteCascades(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	fileRepo, err := postgres.NewDownloadableFileRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadableFileRepo: %v", err)
	}
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}

	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "Ebook Cascade", "ebook-cascade-dl")
	p.Type = catalog.TypeDownloadable
	p.Status = catalog.StatusDraft
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	asset, err := media.NewAsset(id.New(), "files/ebook-cascade.pdf", "ebook-cascade.pdf", "application/pdf", 1024)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}

	f, err := catalog.NewDownloadableFile(p.ID, asset.ID)
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	if err := fileRepo.Upsert(ctx, f); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	n, err := fileRepo.CountByProductID(ctx, p.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountByProductID before delete = %d, %v", n, err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM products WHERE id = $1`, p.ID); err != nil {
		t.Fatalf("DELETE FROM products: %v", err)
	}
	n, err = fileRepo.CountByProductID(ctx, p.ID)
	if err != nil || n != 0 {
		t.Fatalf("CountByProductID after product delete = %d, %v (want 0 via ON DELETE CASCADE)", n, err)
	}
}

func TestDownloadableFileRepo_DeleteAssetRestricted(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	fileRepo, err := postgres.NewDownloadableFileRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadableFileRepo: %v", err)
	}
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}

	ctx := catalog.WithOperatorProductReadScope(context.Background())
	p := mustNewProduct(t, "Ebook", "ebook-restrict-asset")
	p.Type = catalog.TypeDownloadable
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	asset, err := media.NewAsset(id.New(), "files/ebook.pdf", "ebook.pdf", "application/pdf", 1024)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}
	f, err := catalog.NewDownloadableFile(p.ID, asset.ID)
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	if err := fileRepo.Upsert(ctx, f); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	err = assetRepo.Delete(ctx, asset.ID)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), "attached") {
		t.Fatalf("Delete asset = %v, want attached validation", err)
	}
	n, err := fileRepo.CountByAssetID(ctx, asset.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountByAssetID after rejected delete = %d, %v", n, err)
	}
}
