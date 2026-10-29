package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
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

func TestDownloadGrantRepo_InsertConflictDoesNotAbortTx(t *testing.T) {
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
	asset, err := media.NewAsset(id.New(), "files/conflict.pdf", "conflict.pdf", "application/pdf", 10)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}
	orderItemID := download.OrderItemID(id.New(), "var-conflict")
	expires := time.Now().UTC().Add(time.Hour)
	first, err := download.NewDownloadGrant(id.New(), orderItemID, "c", asset.ID, expires, 1)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	txRepo := repo.WithTx(tx)
	dup, err := download.NewDownloadGrant(id.New(), orderItemID, "c", asset.ID, expires, 1)
	if err != nil {
		t.Fatalf("NewDownloadGrant dup: %v", err)
	}
	err = txRepo.Insert(ctx, dup)
	if !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("dup Insert = %v, want conflict", err)
	}
	// TX must remain usable after conflict (ON CONFLICT DO NOTHING).
	got, err := txRepo.FindByOrderItemAndAsset(ctx, orderItemID, asset.ID)
	if err != nil || got == nil || got.ID != first.ID {
		t.Fatalf("reload after conflict = %+v %v", got, err)
	}
}

func TestDownloadGrantRepo_UpdateCountRejectsExpired_DeleteExpiredOrRevoked(t *testing.T) {
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
	asset, err := media.NewAsset(id.New(), "files/purge.pdf", "purge.pdf", "application/pdf", 10)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}
	g, err := download.NewDownloadGrant(id.New(), download.OrderItemID(id.New(), "v"), "c", asset.ID, time.Now().UTC().Add(time.Hour), 3)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := repo.Insert(ctx, g); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := repo.UpdateCount(ctx, g.ID, 1, 0); err != nil {
		t.Fatalf("UpdateCount live: %v", err)
	}
	got, _ := repo.FindByID(ctx, g.ID)
	if err := got.Revoke(time.Now().UTC()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update revoke: %v", err)
	}
	if err := repo.UpdateCount(ctx, g.ID, 2, 1); !errors.Is(err, download.ErrCountConflict) {
		t.Fatalf("UpdateCount after revoke = %v, want ErrCountConflict", err)
	}
	n, err := repo.CountByAssetID(ctx, asset.ID)
	if err != nil || n != 0 {
		t.Fatalf("live count = %d %v, want 0", n, err)
	}
	deleted, err := repo.DeleteExpiredOrRevokedByAssetID(ctx, asset.ID)
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteExpiredOrRevoked = %d %v, want 1", deleted, err)
	}
	got, err = repo.FindByID(ctx, g.ID)
	if err != nil || got != nil {
		t.Fatalf("grant should be purged: %+v %v", got, err)
	}
}

func TestDownloadGrantRepo_ClaimDownload(t *testing.T) {
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
	asset, err := media.NewAsset(id.New(), "private/downloads/claim/ebook.pdf", "ebook.pdf", "application/pdf", 1024)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}

	expires := time.Now().UTC().Add(48 * time.Hour)
	g, err := download.NewDownloadGrant(id.New(), download.OrderItemID(id.New(), "var-1"), "owner-1", asset.ID, expires, 3)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := repo.Insert(ctx, g); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	claimed, err := repo.ClaimDownload(ctx, g.ID, "owner-1")
	if err != nil {
		t.Fatalf("ClaimDownload owner: %v", err)
	}
	if claimed == nil || claimed.DownloadCount != 1 {
		t.Fatalf("claimed = %+v, want count 1", claimed)
	}

	_, err = repo.ClaimDownload(ctx, g.ID, "other-customer")
	if !errors.Is(err, download.ErrCountConflict) {
		t.Fatalf("wrong customer = %v, want ErrCountConflict", err)
	}
	got, _ := repo.FindByID(ctx, g.ID)
	if got == nil || got.DownloadCount != 1 {
		t.Fatalf("count after wrong customer = %+v", got)
	}

	// Exhaust remaining slots then claim must conflict.
	if _, err := repo.ClaimDownload(ctx, g.ID, "owner-1"); err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if _, err := repo.ClaimDownload(ctx, g.ID, "owner-1"); err != nil {
		t.Fatalf("claim 3: %v", err)
	}
	_, err = repo.ClaimDownload(ctx, g.ID, "owner-1")
	if !errors.Is(err, download.ErrCountConflict) {
		t.Fatalf("exhausted = %v, want ErrCountConflict", err)
	}

	// Expired grant: insert fresh with past expiry.
	expired, err := download.NewDownloadGrant(id.New(), download.OrderItemID(id.New(), "var-2"), "owner-1", asset.ID, time.Now().UTC().Add(-time.Minute), 5)
	if err != nil {
		t.Fatalf("NewDownloadGrant expired: %v", err)
	}
	if err := repo.Insert(ctx, expired); err != nil {
		t.Fatalf("Insert expired: %v", err)
	}
	_, err = repo.ClaimDownload(ctx, expired.ID, "owner-1")
	if !errors.Is(err, download.ErrCountConflict) {
		t.Fatalf("expired claim = %v, want ErrCountConflict", err)
	}

	// Concurrent claims stop at download_limit.
	limit := 5
	conc, err := download.NewDownloadGrant(id.New(), download.OrderItemID(id.New(), "var-3"), "owner-1", asset.ID, expires, limit)
	if err != nil {
		t.Fatalf("NewDownloadGrant concurrent: %v", err)
	}
	if err := repo.Insert(ctx, conc); err != nil {
		t.Fatalf("Insert concurrent: %v", err)
	}
	var (
		wg      sync.WaitGroup
		okCount atomic.Int64
	)
	workers := 20
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := repo.ClaimDownload(ctx, conc.ID, "owner-1"); err == nil {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := okCount.Load(); got != int64(limit) {
		t.Fatalf("concurrent successes = %d, want %d", got, limit)
	}
	final, err := repo.FindByID(ctx, conc.ID)
	if err != nil || final == nil || final.DownloadCount != limit {
		t.Fatalf("final count = %+v %v, want %d", final, err, limit)
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

func mustSaveAsset(t *testing.T, ctx context.Context, repo *postgres.AssetRepo, name string) *media.Asset {
	t.Helper()
	a, err := media.NewAsset(id.New(), "files/"+name, name, "application/pdf", 10)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := repo.Save(ctx, &a); err != nil {
		t.Fatalf("Save asset: %v", err)
	}
	return &a
}

func mustInsertGrant(t *testing.T, ctx context.Context, repo *postgres.DownloadGrantRepo, orderItemID, customerID, assetID string, expires time.Time) *download.DownloadGrant {
	t.Helper()
	g, err := download.NewDownloadGrant(id.New(), orderItemID, customerID, assetID, expires, 3)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := repo.Insert(ctx, g); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return g
}

func TestDownloadGrantRepo_RetargetAsset_SimpleLive(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)
	ctx := context.Background()
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	oldA := mustSaveAsset(t, ctx, assetRepo, "retarget-old.pdf")
	newA := mustSaveAsset(t, ctx, assetRepo, "retarget-new.pdf")
	orderItemID := download.OrderItemID(id.New(), "var-simple")
	g := mustInsertGrant(t, ctx, repo, orderItemID, "cust", oldA.ID, time.Now().UTC().Add(time.Hour))

	moved, revoked, err := repo.RetargetAsset(ctx, oldA.ID, newA.ID)
	if err != nil {
		t.Fatalf("RetargetAsset: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved = %d, want 1", moved)
	}
	if len(revoked) != 0 {
		t.Fatalf("revoked = %+v, want none", revoked)
	}
	got, err := repo.FindByID(ctx, g.ID)
	if err != nil || got == nil || got.AssetID != newA.ID {
		t.Fatalf("after retarget = %+v %v", got, err)
	}
	if !got.ExpiresAt.After(time.Now().UTC()) {
		t.Fatal("live grant must stay live")
	}
}

func TestDownloadGrantRepo_RetargetAsset_BundleParentUnit(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)
	ctx := context.Background()
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	oldA := mustSaveAsset(t, ctx, assetRepo, "bundle-old.pdf")
	newA := mustSaveAsset(t, ctx, assetRepo, "bundle-new.pdf")
	// Issuer writes leaf grants under parent bundle order_item_id
	// (orderID/bundleVariantID) — no leaf product join.
	orderItemID := download.OrderItemID(id.New(), "bundle-variant")
	g := mustInsertGrant(t, ctx, repo, orderItemID, "cust", oldA.ID, time.Now().UTC().Add(time.Hour))

	moved, revoked, err := repo.RetargetAsset(ctx, oldA.ID, newA.ID)
	if err != nil {
		t.Fatalf("RetargetAsset: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved = %d, want 1", moved)
	}
	if len(revoked) != 0 {
		t.Fatalf("revoked = %+v", revoked)
	}
	got, err := repo.FindByID(ctx, g.ID)
	if err != nil || got == nil || got.AssetID != newA.ID {
		t.Fatalf("bundle-parent grant = %+v %v", got, err)
	}
}

func TestDownloadGrantRepo_RetargetAsset_LiveConflictRevokesOld(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)
	ctx := context.Background()
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	oldA := mustSaveAsset(t, ctx, assetRepo, "live-conflict-old.pdf")
	newA := mustSaveAsset(t, ctx, assetRepo, "live-conflict-new.pdf")
	orderItemID := download.OrderItemID(id.New(), "var-live-conflict")
	oldG := mustInsertGrant(t, ctx, repo, orderItemID, "cust", oldA.ID, time.Now().UTC().Add(time.Hour))
	newG := mustInsertGrant(t, ctx, repo, orderItemID, "cust", newA.ID, time.Now().UTC().Add(2*time.Hour))

	moved, revoked, err := repo.RetargetAsset(ctx, oldA.ID, newA.ID)
	if err != nil {
		t.Fatalf("RetargetAsset: %v", err)
	}
	if moved != 0 {
		t.Fatalf("moved = %d, want 0 (live conflict)", moved)
	}
	if len(revoked) != 1 || revoked[0].ID != oldG.ID {
		t.Fatalf("revoked = %+v, want old grant", revoked)
	}
	gotOld, err := repo.FindByID(ctx, oldG.ID)
	if err != nil || gotOld == nil || gotOld.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("old must be revoked: %+v %v", gotOld, err)
	}
	if gotOld.AssetID != oldA.ID {
		t.Fatalf("revoked old keeps old asset_id, got %s", gotOld.AssetID)
	}
	gotNew, err := repo.FindByID(ctx, newG.ID)
	if err != nil || gotNew == nil || gotNew.AssetID != newA.ID || !gotNew.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("live new must remain: %+v %v", gotNew, err)
	}
}

func TestDownloadGrantRepo_RetargetAsset_ExpiredNewDoesNotKillLiveOld(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)
	ctx := context.Background()
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	oldA := mustSaveAsset(t, ctx, assetRepo, "expired-new-old.pdf")
	newA := mustSaveAsset(t, ctx, assetRepo, "expired-new-new.pdf")
	orderItemID := download.OrderItemID(id.New(), "var-expired-new")
	liveOld := mustInsertGrant(t, ctx, repo, orderItemID, "cust", oldA.ID, time.Now().UTC().Add(time.Hour))
	expiredNew := mustInsertGrant(t, ctx, repo, orderItemID, "cust", newA.ID, time.Now().UTC().Add(time.Hour))
	expiredNew.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	if err := repo.Update(ctx, expiredNew); err != nil {
		t.Fatalf("expire new: %v", err)
	}

	moved, revoked, err := repo.RetargetAsset(ctx, oldA.ID, newA.ID)
	if err != nil {
		t.Fatalf("RetargetAsset: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved = %d, want 1", moved)
	}
	if len(revoked) != 0 {
		t.Fatalf("revoked = %+v, want none (expired new cleared)", revoked)
	}
	got, err := repo.FindByID(ctx, liveOld.ID)
	if err != nil || got == nil {
		t.Fatalf("live old missing: %v", err)
	}
	if got.AssetID != newA.ID {
		t.Fatalf("AssetID = %s, want new", got.AssetID)
	}
	if !got.ExpiresAt.After(time.Now().UTC()) {
		t.Fatal("live old must not be killed")
	}
	gone, err := repo.FindByID(ctx, expiredNew.ID)
	if err != nil || gone != nil {
		t.Fatalf("expired new should be deleted: %+v %v", gone, err)
	}
}

func TestDownloadGrantRepo_RetargetAsset_ExpiredOldUpdatedForReissue(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)
	ctx := context.Background()
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	oldA := mustSaveAsset(t, ctx, assetRepo, "expired-old-old.pdf")
	newA := mustSaveAsset(t, ctx, assetRepo, "expired-old-new.pdf")
	orderItemID := download.OrderItemID(id.New(), "var-expired-old")
	expiredOld := mustInsertGrant(t, ctx, repo, orderItemID, "cust", oldA.ID, time.Now().UTC().Add(time.Hour))
	expiredOld.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	if err := repo.Update(ctx, expiredOld); err != nil {
		t.Fatalf("expire old: %v", err)
	}

	moved, revoked, err := repo.RetargetAsset(ctx, oldA.ID, newA.ID)
	if err != nil {
		t.Fatalf("RetargetAsset: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved = %d, want 1 (expired kept when UNIQUE free)", moved)
	}
	if len(revoked) != 0 {
		t.Fatalf("revoked = %+v", revoked)
	}
	got, err := repo.FindByID(ctx, expiredOld.ID)
	if err != nil || got == nil || got.AssetID != newA.ID {
		t.Fatalf("expired old asset_id = %+v %v, want new", got, err)
	}
}

func TestDownloadGrantRepo_RetargetAsset_ExpiredOldDeletedWhenLiveNewBlocks(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)
	ctx := context.Background()
	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	repo, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	oldA := mustSaveAsset(t, ctx, assetRepo, "expired-old-live-new-old.pdf")
	newA := mustSaveAsset(t, ctx, assetRepo, "expired-old-live-new-new.pdf")
	orderItemID := download.OrderItemID(id.New(), "var-expired-old-live-new")
	expiredOld := mustInsertGrant(t, ctx, repo, orderItemID, "cust", oldA.ID, time.Now().UTC().Add(time.Hour))
	expiredOld.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	if err := repo.Update(ctx, expiredOld); err != nil {
		t.Fatalf("expire old: %v", err)
	}
	liveNew := mustInsertGrant(t, ctx, repo, orderItemID, "cust", newA.ID, time.Now().UTC().Add(2*time.Hour))

	moved, revoked, err := repo.RetargetAsset(ctx, oldA.ID, newA.ID)
	if err != nil {
		t.Fatalf("RetargetAsset: %v", err)
	}
	if moved != 0 {
		t.Fatalf("moved = %d, want 0 (UNIQUE blocked)", moved)
	}
	if len(revoked) != 0 {
		t.Fatalf("revoked = %+v, want none (expired old deleted, not revoked; live new untouched)", revoked)
	}
	gone, err := repo.FindByID(ctx, expiredOld.ID)
	if err != nil || gone != nil {
		t.Fatalf("expired old must be deleted (grant id vanishes): %+v %v", gone, err)
	}
	gotNew, err := repo.FindByID(ctx, liveNew.ID)
	if err != nil || gotNew == nil || gotNew.AssetID != newA.ID || !gotNew.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("live new must remain untouched: %+v %v", gotNew, err)
	}
}
