package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestVisibilityFactsRepo_MissingDownloadableFile(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	ctx := context.Background()
	prodRepo, err := postgres.NewProductRepo(db)
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
	facts, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	prod := mustNewProduct(t, "DL Facts", "dl-facts-"+id.New())
	prod.Type = catalog.TypeDownloadable
	prod.Status = catalog.StatusActive
	modes := catalog.DefaultVisibilityAxes()
	modes.Catalog = catalog.VisibilityModeVisible
	modes.Purchasable = catalog.VisibilityModeVisible
	prod.VisibilityModes = modes
	if err := prodRepo.Create(productRepoAdminCtx(), &prod); err != nil {
		t.Fatalf("Create: %v", err)
	}

	snaps, err := facts.LoadManyAnyStatus(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadManyAnyStatus: %v", err)
	}
	snap, ok := snaps[prod.ID]
	if !ok {
		t.Fatal("snapshot missing")
	}
	if !snap.Inputs.MissingDownloadableFile {
		t.Fatal("MissingDownloadableFile = false, want true with zero attachments")
	}
	resolved := snap.Resolve(catalog.VisibilityOptions{})
	if !resolved.VisibleInCatalog {
		t.Fatal("VisibleInCatalog = false, want true")
	}
	if resolved.Purchasable {
		t.Fatal("Purchasable = true, want false while downloadable has no file")
	}

	asset, err := media.NewAsset(id.New(), "files/ebook.pdf", "ebook.pdf", "application/pdf", 100)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}
	f, err := catalog.NewDownloadableFile(prod.ID, asset.ID)
	if err != nil {
		t.Fatalf("NewDownloadableFile: %v", err)
	}
	if err := fileRepo.Upsert(ctx, f); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	snaps, err = facts.LoadManyAnyStatus(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadManyAnyStatus after attach: %v", err)
	}
	snap = snaps[prod.ID]
	if snap.Inputs.MissingDownloadableFile {
		t.Fatal("MissingDownloadableFile = true, want false after attach")
	}
	resolved = snap.Resolve(catalog.VisibilityOptions{})
	if !resolved.Purchasable {
		t.Fatal("Purchasable = false, want true after attach with VisibilityModeVisible")
	}
}

func TestVisibilityFactsRepo_SimpleProductMissingDownloadableFileFalse(t *testing.T) {
	db := testDB(t)
	ensureProductsTable(t, db)

	ctx := context.Background()
	prodRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	facts, err := postgres.NewVisibilityFactsRepo(db)
	if err != nil {
		t.Fatalf("NewVisibilityFactsRepo: %v", err)
	}

	prod := mustNewProduct(t, "Simple Facts", "simple-facts-"+id.New())
	prod.Type = catalog.TypeSimple
	prod.Status = catalog.StatusActive
	prod.VisibilityModes = catalog.DefaultVisibilityAxes()
	if err := prodRepo.Create(productRepoAdminCtx(), &prod); err != nil {
		t.Fatalf("Create: %v", err)
	}

	snaps, err := facts.LoadManyAnyStatus(ctx, []string{prod.ID})
	if err != nil {
		t.Fatalf("LoadManyAnyStatus: %v", err)
	}
	snap, ok := snaps[prod.ID]
	if !ok {
		t.Fatal("snapshot missing")
	}
	if snap.Inputs.MissingDownloadableFile {
		t.Fatal("MissingDownloadableFile = true on simple product, want false")
	}
}
