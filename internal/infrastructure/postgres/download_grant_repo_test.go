package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func ensureDownloadGrantsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM download_grants")
		mustExec(t, db, "DELETE FROM downloadable_grant_settings")
	})
}

func TestDownloadGrantRepo_Migration105Applied(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)

	var rel sql.NullString
	if err := db.QueryRow(`SELECT to_regclass($1)::text`, "public.download_grants").Scan(&rel); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	if !rel.Valid || rel.String != "download_grants" {
		t.Fatalf("download_grants missing (migration 105 not applied): %v", rel)
	}
	if err := db.QueryRow(`SELECT to_regclass($1)::text`, "public.downloadable_grant_settings").Scan(&rel); err != nil {
		t.Fatalf("to_regclass settings: %v", err)
	}
	if !rel.Valid || rel.String != "downloadable_grant_settings" {
		t.Fatalf("downloadable_grant_settings missing: %v", rel)
	}
}

func TestDownloadGrantRepo_InsertFindUpdateCount(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)

	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	ctx := context.Background()
	asset, err := media.NewAsset(id.New(), "files/ebook.pdf", "ebook.pdf", "application/pdf", 1024)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}

	orderItemID := download.OrderItemID(id.New(), "var-1")
	expires := time.Now().UTC().Add(48 * time.Hour)
	g, err := download.NewDownloadGrant(id.New(), orderItemID, "cust-1", asset.ID, expires, 3)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := repo.Insert(ctx, g); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := repo.FindByID(ctx, g.ID)
	if err != nil || got == nil || got.AssetID != asset.ID || got.CustomerID != "cust-1" {
		t.Fatalf("FindByID = %+v %v", got, err)
	}
	byPair, err := repo.FindByOrderItemAndAsset(ctx, orderItemID, asset.ID)
	if err != nil || byPair == nil || byPair.ID != g.ID {
		t.Fatalf("FindByOrderItemAndAsset = %+v %v", byPair, err)
	}

	dup, err := download.NewDownloadGrant(id.New(), orderItemID, "cust-1", asset.ID, expires, 3)
	if err != nil {
		t.Fatalf("NewDownloadGrant dup: %v", err)
	}
	err = repo.Insert(ctx, dup)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("duplicate insert = %v, want conflict", err)
	}

	if err := repo.UpdateCount(ctx, g.ID, 1, 0); err != nil {
		t.Fatalf("UpdateCount: %v", err)
	}
	if err := repo.UpdateCount(ctx, g.ID, 2, 0); !errors.Is(err, download.ErrCountConflict) {
		t.Fatalf("stale UpdateCount = %v", err)
	}

	got, _ = repo.FindByID(ctx, g.ID)
	got.DownloadCount = 1
	if err := got.Revoke(time.Now().UTC()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update revoke: %v", err)
	}

	list, err := repo.ListByCustomerID(ctx, "cust-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByCustomerID = %d %v", len(list), err)
	}
	byItem, err := repo.ListByOrderItemID(ctx, orderItemID)
	if err != nil || len(byItem) != 1 {
		t.Fatalf("ListByOrderItemID = %d %v", len(byItem), err)
	}
}

func TestDownloadGrantRepo_WithTx_RollbackInvisible(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)

	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	ctx := context.Background()
	asset, err := media.NewAsset(id.New(), "files/tx.pdf", "tx.pdf", "application/pdf", 10)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}
	g, err := download.NewDownloadGrant(id.New(), download.OrderItemID(id.New(), "v"), "c", asset.ID, time.Now().UTC().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.WithTx(tx).Insert(ctx, g); err != nil {
		t.Fatalf("Insert in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got, err := repo.FindByID(ctx, g.ID)
	if err != nil || got != nil {
		t.Fatalf("rolled-back insert visible: %+v %v", got, err)
	}
}

func TestDownloadableGrantSettingsRepo_UpsertFindRemove(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)

	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		t.Fatalf("NewProductRepo: %v", err)
	}
	settingsRepo, err := postgres.NewDownloadableGrantSettingsRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadableGrantSettingsRepo: %v", err)
	}
	ctx := context.Background()
	p := mustNewProduct(t, "DL Settings", "dl-settings")
	p.Type = catalog.TypeDownloadable
	if err := productRepo.Create(ctx, &p); err != nil {
		t.Fatalf("Create product: %v", err)
	}

	days := 14
	limit := 7
	s := &download.GrantSettings{ProductID: p.ID, ExpiryDays: &days, DownloadLimit: &limit}
	if err := settingsRepo.Upsert(ctx, s); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := settingsRepo.FindByProductID(ctx, p.ID)
	if err != nil || got == nil || got.ExpiryDays == nil || *got.ExpiryDays != 14 || got.DownloadLimit == nil || *got.DownloadLimit != 7 {
		t.Fatalf("Find = %+v %v", got, err)
	}
	if err := settingsRepo.Remove(ctx, p.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, err = settingsRepo.FindByProductID(ctx, p.ID)
	if err != nil || got != nil {
		t.Fatalf("after remove = %+v %v", got, err)
	}
}
