package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	downloadapp "github.com/akarso/shopanda/internal/application/download"
	"github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// failCountGrantRepo wraps DownloadGrantRepo and forces UpdateCount to fail
// so Reissue's real sql.Tx path can assert rollback of Extend.
type failCountGrantRepo struct {
	base *postgres.DownloadGrantRepo
	fail bool
}

func (r *failCountGrantRepo) WithTx(tx *sql.Tx) download.Repository {
	bound, ok := r.base.WithTx(tx).(*postgres.DownloadGrantRepo)
	if !ok {
		panic("WithTx did not return *DownloadGrantRepo")
	}
	return &failCountGrantRepo{base: bound, fail: r.fail}
}

func (r *failCountGrantRepo) Insert(ctx context.Context, g *download.DownloadGrant) error {
	return r.base.Insert(ctx, g)
}
func (r *failCountGrantRepo) Update(ctx context.Context, g *download.DownloadGrant) error {
	return r.base.Update(ctx, g)
}
func (r *failCountGrantRepo) UpdateCount(ctx context.Context, id string, newCount, expectedCount int) error {
	if r.fail {
		return download.ErrCountConflict
	}
	return r.base.UpdateCount(ctx, id, newCount, expectedCount)
}
func (r *failCountGrantRepo) ClaimDownload(ctx context.Context, id, customerID string) (*download.DownloadGrant, error) {
	return r.base.ClaimDownload(ctx, id, customerID)
}
func (r *failCountGrantRepo) FindByID(ctx context.Context, id string) (*download.DownloadGrant, error) {
	return r.base.FindByID(ctx, id)
}
func (r *failCountGrantRepo) FindByOrderItemAndAsset(ctx context.Context, orderItemID, assetID string) (*download.DownloadGrant, error) {
	return r.base.FindByOrderItemAndAsset(ctx, orderItemID, assetID)
}
func (r *failCountGrantRepo) ListByCustomerID(ctx context.Context, customerID string) ([]download.DownloadGrant, error) {
	return r.base.ListByCustomerID(ctx, customerID)
}
func (r *failCountGrantRepo) ListByOrderItemID(ctx context.Context, orderItemID string) ([]download.DownloadGrant, error) {
	return r.base.ListByOrderItemID(ctx, orderItemID)
}
func (r *failCountGrantRepo) CountByAssetID(ctx context.Context, assetID string) (int, error) {
	return r.base.CountByAssetID(ctx, assetID)
}
func (r *failCountGrantRepo) DeleteExpiredOrRevokedByAssetID(ctx context.Context, assetID string) (int, error) {
	return r.base.DeleteExpiredOrRevokedByAssetID(ctx, assetID)
}
func (r *failCountGrantRepo) LockForUpdate(ctx context.Context, id string) (*download.DownloadGrant, error) {
	return r.base.LockForUpdate(ctx, id)
}

func TestService_Reissue_TxRollbackOnUpdateCountFail(t *testing.T) {
	db := testDB(t)
	ensureDownloadGrantsTable(t, db)
	ensureProductsTable(t, db)
	ctx := context.Background()

	assetRepo, err := postgres.NewAssetRepo(db)
	if err != nil {
		t.Fatalf("NewAssetRepo: %v", err)
	}
	base, err := postgres.NewDownloadGrantRepo(db)
	if err != nil {
		t.Fatalf("NewDownloadGrantRepo: %v", err)
	}
	asset, err := media.NewAsset(id.New(), "files/reissue-rollback.pdf", "reissue-rollback.pdf", "application/pdf", 10)
	if err != nil {
		t.Fatalf("NewAsset: %v", err)
	}
	if err := assetRepo.Save(ctx, &asset); err != nil {
		t.Fatalf("Save asset: %v", err)
	}

	originalExpiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	g, err := download.NewDownloadGrant(id.New(), download.OrderItemID(id.New(), "v-reissue"), "cust", asset.ID, originalExpiry, 5)
	if err != nil {
		t.Fatalf("NewDownloadGrant: %v", err)
	}
	if err := base.Insert(ctx, g); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := base.UpdateCount(ctx, g.ID, 2, 0); err != nil {
		t.Fatalf("seed count: %v", err)
	}

	failRepo := &failCountGrantRepo{base: base, fail: true}
	svc := downloadapp.NewService(failRepo, nil).WithTxStarter(db)
	later := time.Now().UTC().Add(72 * time.Hour)
	limit := 10
	_, err = svc.Reissue(ctx, g.ID, later, &limit, true, "rollback")
	if err == nil || !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("Reissue = %v, want conflict", err)
	}

	got, err := base.FindByID(ctx, g.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %+v %v", got, err)
	}
	if got.DownloadCount != 2 {
		t.Fatalf("DownloadCount = %d, want 2 (rolled back)", got.DownloadCount)
	}
	if got.DownloadLimit != 5 {
		t.Fatalf("DownloadLimit = %d, want 5 (rolled back)", got.DownloadLimit)
	}
	if !got.ExpiresAt.Equal(originalExpiry) && got.ExpiresAt.Sub(originalExpiry).Abs() > time.Millisecond {
		// Compare with DB precision; Truncate may still differ slightly via scan.
		if got.ExpiresAt.After(originalExpiry.Add(time.Second)) || got.ExpiresAt.Before(originalExpiry.Add(-time.Second)) {
			t.Fatalf("ExpiresAt = %v, want ~%v (rolled back)", got.ExpiresAt, originalExpiry)
		}
	}
	if got.ExpiresAt.Equal(later) || got.ExpiresAt.Sub(later).Abs() < time.Second {
		t.Fatalf("ExpiresAt was committed to reissue value %v", got.ExpiresAt)
	}
}
