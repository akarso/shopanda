package download_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	downloadapp "github.com/akarso/shopanda/internal/application/download"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type memGrantRepo struct {
	mu      sync.Mutex
	byID    map[string]*domain.DownloadGrant
	byPair  map[string]*domain.DownloadGrant
	failIns error
}

var _ domain.Repository = (*memGrantRepo)(nil)

func newMemGrantRepo() *memGrantRepo {
	return &memGrantRepo{
		byID:   map[string]*domain.DownloadGrant{},
		byPair: map[string]*domain.DownloadGrant{},
	}
}

func pairKey(orderItemID, assetID string) string {
	return orderItemID + "\x00" + assetID
}

func (m *memGrantRepo) Insert(_ context.Context, g *domain.DownloadGrant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failIns != nil {
		return m.failIns
	}
	if err := g.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	key := pairKey(g.OrderItemID, g.AssetID)
	if _, ok := m.byPair[key]; ok {
		return apperror.Conflict("download grant already exists")
	}
	cp := *g
	m.byID[g.ID] = &cp
	m.byPair[key] = &cp
	return nil
}

func (m *memGrantRepo) Update(_ context.Context, g *domain.DownloadGrant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := g.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	existing, ok := m.byID[g.ID]
	if !ok {
		return apperror.NotFound("download grant not found")
	}
	cp := *g
	cp.DownloadCount = existing.DownloadCount // Update does not clobber count
	m.byID[g.ID] = &cp
	m.byPair[pairKey(g.OrderItemID, g.AssetID)] = &cp
	return nil
}

func (m *memGrantRepo) UpdateCount(_ context.Context, id string, newCount, expectedCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.byID[id]
	if !ok {
		return domain.ErrCountConflict
	}
	// Match postgres: CAS only while download_count matches and expires_at > now.
	if g.DownloadCount != expectedCount || !g.ExpiresAt.After(time.Now().UTC()) {
		return domain.ErrCountConflict
	}
	g.DownloadCount = newCount
	return nil
}

func (m *memGrantRepo) ClaimDownload(_ context.Context, id, customerID string) (*domain.DownloadGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrCountConflict
	}
	if g.CustomerID != customerID || g.Expired(time.Now().UTC()) || g.Exhausted() {
		return nil, domain.ErrCountConflict
	}
	g.DownloadCount++
	cp := *g
	return &cp, nil
}

func (m *memGrantRepo) FindByID(_ context.Context, id string) (*domain.DownloadGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *g
	return &cp, nil
}

func (m *memGrantRepo) FindByOrderItemAndAsset(_ context.Context, orderItemID, assetID string) (*domain.DownloadGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.byPair[pairKey(orderItemID, assetID)]
	if !ok {
		return nil, nil
	}
	cp := *g
	return &cp, nil
}

func (m *memGrantRepo) ListByCustomerID(_ context.Context, customerID string) ([]domain.DownloadGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.DownloadGrant{}
	for _, g := range m.byID {
		if g.CustomerID == customerID {
			out = append(out, *g)
		}
	}
	return out, nil
}

func (m *memGrantRepo) ListByOrderItemID(_ context.Context, orderItemID string) ([]domain.DownloadGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.DownloadGrant{}
	for _, g := range m.byID {
		if g.OrderItemID == orderItemID {
			out = append(out, *g)
		}
	}
	return out, nil
}

func (m *memGrantRepo) CountByAssetID(_ context.Context, assetID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	n := 0
	for _, g := range m.byID {
		if g.AssetID == assetID && !g.Expired(now) {
			n++
		}
	}
	return n, nil
}

func (m *memGrantRepo) DeleteExpiredOrRevokedByAssetID(_ context.Context, assetID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	n := 0
	for id, g := range m.byID {
		if g.AssetID == assetID && g.Expired(now) {
			delete(m.byID, id)
			delete(m.byPair, pairKey(g.OrderItemID, g.AssetID))
			n++
		}
	}
	return n, nil
}

func TestService_IssueGetRevokeRecord(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	orderID := id.New()
	orderItemID := domain.OrderItemID(orderID, "var-1")
	expires := time.Now().UTC().Add(24 * time.Hour)

	g, err := svc.Issue(context.Background(), orderItemID, "cust-1", "asset-1", expires, 2)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, err := svc.Get(context.Background(), g.ID)
	if err != nil || got.AssetID != "asset-1" {
		t.Fatalf("Get: %+v %v", got, err)
	}

	got, err = svc.RecordDownload(context.Background(), g.ID)
	if err != nil || got.DownloadCount != 1 {
		t.Fatalf("RecordDownload: %+v %v", got, err)
	}

	_, existed, err := svc.IssueIdempotent(context.Background(), orderItemID, "cust-1", "asset-1", expires, 2)
	if err != nil || !existed {
		t.Fatalf("IssueIdempotent = existed=%v err=%v", existed, err)
	}

	revoked, idempotent, err := svc.Revoke(context.Background(), g.ID, "support")
	if err != nil || idempotent || !revoked.Expired(time.Now().UTC()) {
		t.Fatalf("Revoke: %+v idem=%v err=%v", revoked, idempotent, err)
	}
	_, idempotent, err = svc.Revoke(context.Background(), g.ID, "support")
	if err != nil || !idempotent {
		t.Fatalf("second Revoke: idem=%v err=%v", idempotent, err)
	}
}

func TestService_Issue_Validation(t *testing.T) {
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	_, err := svc.Issue(context.Background(), "", "c", "a", time.Now().UTC().Add(time.Hour), 1)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty order item: %v", err)
	}
	_, err = svc.Get(context.Background(), "not-uuid")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("bad id: %v", err)
	}
	_, err = svc.Get(context.Background(), id.New())
	if err == nil || !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestService_UpdateCountConflictMapping(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", time.Now().UTC().Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := repo.UpdateCount(context.Background(), g.ID, 1, 99); err == nil || !errors.Is(err, domain.ErrCountConflict) {
		t.Fatalf("UpdateCount: %v", err)
	}
	_ = svc
}

func TestService_ListByCustomer_EmptyRejected(t *testing.T) {
	svc := downloadapp.NewService(newMemGrantRepo(), nil)
	_, err := svc.ListByCustomer(context.Background(), "  ")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestService_RecordDownload_ExpiredExhaustedViaErrorsIs(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	expired, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v1"), "c", "a1", now.Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	expired.ExpiresAt = now.Add(-time.Second)
	_ = repo.Update(context.Background(), expired)

	_, err = svc.RecordDownload(context.Background(), expired.ID)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) || !errors.Is(err, domain.ErrExpired) {
		t.Fatalf("expired RecordDownload = %v", err)
	}

	live, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v2"), "c", "a2", now.Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("Issue live: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), live.ID); err != nil {
		t.Fatalf("first download: %v", err)
	}
	_, err = svc.RecordDownload(context.Background(), live.ID)
	if err == nil || !apperror.Is(err, apperror.CodeValidation) || !errors.Is(err, domain.ErrExhausted) {
		t.Fatalf("exhausted RecordDownload = %v", err)
	}
}

type casConflictRepo struct {
	*memGrantRepo
	forceConflict bool
}

func (c *casConflictRepo) UpdateCount(ctx context.Context, id string, newCount, expectedCount int) error {
	if c.forceConflict {
		return domain.ErrCountConflict
	}
	return c.memGrantRepo.UpdateCount(ctx, id, newCount, expectedCount)
}

func (c *casConflictRepo) ClaimDownload(ctx context.Context, id, customerID string) (*domain.DownloadGrant, error) {
	if c.forceConflict {
		return nil, domain.ErrCountConflict
	}
	return c.memGrantRepo.ClaimDownload(ctx, id, customerID)
}

func TestService_RecordDownload_CASConflictThroughService(t *testing.T) {
	base := newMemGrantRepo()
	repo := &casConflictRepo{memGrantRepo: base}
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", time.Now().UTC().Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	repo.forceConflict = true
	_, err = svc.RecordDownload(context.Background(), g.ID)
	if err == nil || !apperror.Is(err, apperror.CodeConflict) || !errors.Is(err, domain.ErrCountConflict) {
		t.Fatalf("CAS via Service.RecordDownload = %v", err)
	}
}

func TestService_UpdateDoesNotClobberCount_UpdateCountRejectsRevoked(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", time.Now().UTC().Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	// Stale in-memory revoke snapshot with count=0 must not wipe the recorded download.
	stale, err := svc.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	stale.DownloadCount = 0
	if err := stale.Revoke(time.Now().UTC()); err != nil {
		t.Fatalf("Revoke domain: %v", err)
	}
	if err := repo.Update(context.Background(), stale); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := svc.Get(context.Background(), g.ID)
	if err != nil || got.DownloadCount != 1 {
		t.Fatalf("count after Update = %+v %v, want download_count=1", got, err)
	}
	if err := repo.UpdateCount(context.Background(), g.ID, 2, 1); !errors.Is(err, domain.ErrCountConflict) {
		t.Fatalf("UpdateCount on revoked = %v, want ErrCountConflict", err)
	}
}

func TestService_IssueIdempotent_LeavesRevokedUntouched(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	orderItemID := domain.OrderItemID(id.New(), "v")
	expires := time.Now().UTC().Add(24 * time.Hour)
	g, err := svc.Issue(context.Background(), orderItemID, "cust", "asset", expires, 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	revoked, _, err := svc.Revoke(context.Background(), g.ID, "test")
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revokedAt := revoked.ExpiresAt
	later := time.Now().UTC().Add(48 * time.Hour)
	got, existed, err := svc.IssueIdempotent(context.Background(), orderItemID, "cust", "asset", later, 7)
	if err != nil || !existed {
		t.Fatalf("IssueIdempotent existed=%v err=%v, want existed=true", existed, err)
	}
	if got.DownloadLimit != 5 || got.DownloadCount != 0 || !got.ExpiresAt.Equal(revokedAt) {
		t.Fatalf("revoked grant must be unchanged: %+v", got)
	}
	if !got.Expired(time.Now().UTC()) {
		t.Fatal("grant must remain expired/revoked")
	}
}

func TestService_Reissue_ExtendThenReset(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", now.Add(time.Hour), 3)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	if _, _, err := svc.Revoke(context.Background(), g.ID, "lost"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	later := now.Add(72 * time.Hour)
	limit := 10
	got, err := svc.Reissue(context.Background(), g.ID, later, &limit, true, "support")
	if err != nil {
		t.Fatalf("Reissue: %v", err)
	}
	if got.DownloadCount != 0 {
		t.Fatalf("DownloadCount = %d, want 0", got.DownloadCount)
	}
	if got.DownloadLimit != 10 {
		t.Fatalf("DownloadLimit = %d, want 10", got.DownloadLimit)
	}
	if !got.ExpiresAt.Equal(later) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, later)
	}
	if err := got.CanDownload(now.Add(time.Minute)); err != nil {
		t.Fatalf("CanDownload after reissue: %v", err)
	}
}

func TestService_Reissue_LowerLimitWithReset(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", now.Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload 2: %v", err)
	}
	later := now.Add(48 * time.Hour)
	limit := 1
	got, err := svc.Reissue(context.Background(), g.ID, later, &limit, true, "lower")
	if err != nil {
		t.Fatalf("Reissue lower+reset: %v", err)
	}
	if got.DownloadCount != 0 || got.DownloadLimit != 1 {
		t.Fatalf("got count=%d limit=%d, want 0/1", got.DownloadCount, got.DownloadLimit)
	}
}

func TestService_Reissue_PastExpiryRejected(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", now.Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	_, err = svc.Reissue(context.Background(), g.ID, now.Add(-time.Minute), nil, true, "past")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("past expiry = %v, want validation", err)
	}
}

func TestService_Reissue_ResetCountFalseKeepsCount(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", now.Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	later := now.Add(48 * time.Hour)
	got, err := svc.Reissue(context.Background(), g.ID, later, nil, false, "extend only")
	if err != nil {
		t.Fatalf("Reissue reset_count=false: %v", err)
	}
	if got.DownloadCount != 1 {
		t.Fatalf("DownloadCount = %d, want 1", got.DownloadCount)
	}
}

func TestService_Reissue_ResetCountFalseLowerLimitRejected(t *testing.T) {
	repo := newMemGrantRepo()
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", now.Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload 2: %v", err)
	}
	limit := 1
	_, err = svc.Reissue(context.Background(), g.ID, now.Add(48*time.Hour), &limit, false, "bad")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("lower limit without reset = %v, want validation", err)
	}
}

func TestService_Reissue_CASConflict(t *testing.T) {
	base := newMemGrantRepo()
	repo := &casConflictRepo{memGrantRepo: base}
	svc := downloadapp.NewService(repo, nil)
	now := time.Now().UTC()
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", now.Add(time.Hour), 5)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := svc.RecordDownload(context.Background(), g.ID); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	repo.forceConflict = true
	_, err = svc.Reissue(context.Background(), g.ID, now.Add(48*time.Hour), nil, true, "cas")
	if err == nil || !apperror.Is(err, apperror.CodeConflict) || !errors.Is(err, domain.ErrCountConflict) {
		t.Fatalf("CAS conflict = %v", err)
	}
}

type withTxMemRepo struct {
	*memGrantRepo
}

func (w *withTxMemRepo) WithTx(*sql.Tx) domain.Repository {
	return w
}

func TestService_Reissue_RequiresTxStarterWhenRepoBindsTx(t *testing.T) {
	repo := &withTxMemRepo{memGrantRepo: newMemGrantRepo()}
	svc := downloadapp.NewService(repo, nil) // no WithTxStarter
	now := time.Now().UTC()
	g, err := svc.Issue(context.Background(), domain.OrderItemID(id.New(), "v"), "c", "a", now.Add(time.Hour), 3)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	_, err = svc.Reissue(context.Background(), g.ID, now.Add(48*time.Hour), nil, true, "need tx")
	if err == nil || !apperror.Is(err, apperror.CodeInternal) {
		t.Fatalf("Reissue without txStarter = %v, want internal", err)
	}
}
