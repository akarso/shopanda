package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// eventBus is the subset of *event.Bus used for grant observability.
type eventBus interface {
	Publish(ctx context.Context, evt event.Event) error
}

// Service manages DownloadGrant rows: issue (manual), read, record
// download, extend, revoke, and reset. Order-paid issuance lives on
// Issuer (HandleOrderPaid). Delivery HTTP is PR-1077; admin HTTP is PR-1078.
type Service struct {
	grants domain.Repository
	bus    eventBus
	now    func() time.Time
}

// NewService creates a DownloadGrant service. bus may be nil (no events).
func NewService(grants domain.Repository, bus eventBus) *Service {
	if grants == nil {
		panic("download.NewService: nil grants repository")
	}
	return &Service{
		grants: grants,
		bus:    bus,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// withRepo returns a shallow copy bound to a different repository (tx).
func (s *Service) withRepo(grants domain.Repository) *Service {
	if s == nil {
		return nil
	}
	return &Service{grants: grants, bus: s.bus, now: s.now}
}

// WithTx returns a service bound to the grant repo's transaction when the
// underlying postgres adapter supports WithTx. Used by returns revoke.
func (s *Service) WithTx(tx *sql.Tx) *Service {
	if s == nil {
		return nil
	}
	if binder, ok := s.grants.(interface {
		WithTx(tx *sql.Tx) domain.Repository
	}); ok {
		return s.withRepo(binder.WithTx(tx))
	}
	return s
}

// BindsTx reports whether the grant repository supports WithTx.
func (s *Service) BindsTx() bool {
	if s == nil {
		return false
	}
	_, ok := s.grants.(interface {
		WithTx(tx *sql.Tx) domain.Repository
	})
	return ok
}

// WithTxRepository returns the grant repository bound to tx when the adapter
// supports WithTx. Used by media.Delete so purge + live count + asset row
// delete share one transaction.
func (s *Service) WithTxRepository(tx *sql.Tx) domain.Repository {
	bound := s.WithTx(tx)
	if bound == nil {
		return nil
	}
	return bound.grants
}

// Issue inserts a new grant. A duplicate (order_item_id, asset_id) is a
// conflict — callers that need idempotent create should use IssueIdempotent.
func (s *Service) Issue(ctx context.Context, orderItemID, customerID, assetID string, expiresAt time.Time, downloadLimit int) (*domain.DownloadGrant, error) {
	orderItemID = strings.TrimSpace(orderItemID)
	if canon, ok := domain.CanonicalOrderItemID(orderItemID); ok {
		orderItemID = canon
	}
	g, err := domain.NewDownloadGrant(id.New(), orderItemID, customerID, assetID, expiresAt, downloadLimit)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.grants.Insert(ctx, g); err != nil {
		return nil, mapWriteError("issue", err)
	}
	s.publishIssued(ctx, g, "")
	return g, nil
}

// IssueIdempotent inserts a grant or returns the existing row when
// (order_item_id, asset_id) already exists. Used by order.paid issuance.
//
// On conflict/hit the existing row is returned unchanged (existed=true) —
// including expired/revoked grants. Paid redelivery must not resurrect a
// revoked entitlement. Support re-issue (Extend + ResetCount) is PR-1078.
func (s *Service) IssueIdempotent(ctx context.Context, orderItemID, customerID, assetID string, expiresAt time.Time, downloadLimit int) (*domain.DownloadGrant, bool, error) {
	orderItemID = strings.TrimSpace(orderItemID)
	if canon, ok := domain.CanonicalOrderItemID(orderItemID); ok {
		orderItemID = canon
	}
	assetID = strings.TrimSpace(assetID)
	existing, err := s.grants.FindByOrderItemAndAsset(ctx, orderItemID, assetID)
	if err != nil {
		return nil, false, fmt.Errorf("download: find existing: %w", err)
	}
	if existing != nil {
		return existing, true, nil
	}
	g, err := domain.NewDownloadGrant(id.New(), orderItemID, customerID, assetID, expiresAt, downloadLimit)
	if err != nil {
		return nil, false, apperror.Validation(err.Error())
	}
	if err := s.grants.Insert(ctx, g); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			existing, findErr := s.grants.FindByOrderItemAndAsset(ctx, orderItemID, assetID)
			if findErr != nil {
				return nil, false, fmt.Errorf("download: reload after conflict: %w", findErr)
			}
			if existing != nil {
				return existing, true, nil
			}
		}
		return nil, false, mapWriteError("issue", err)
	}
	// Caller (Issuer / Issue) publishes EventDownloadGrantIssued so order_id
	// can be attached when known. IssueIdempotent itself stays quiet.
	return g, false, nil
}

// Get returns the grant by id.
func (s *Service) Get(ctx context.Context, grantID string) (*domain.DownloadGrant, error) {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return nil, apperror.Validation("download grant id must not be empty")
	}
	if !id.IsUUID(grantID) {
		return nil, apperror.Validation("invalid download grant id")
	}
	g, err := s.grants.FindByID(ctx, grantID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("download: get: %w", err)
	}
	if g == nil {
		return nil, apperror.NotFound("download grant not found")
	}
	return g, nil
}

// ListByCustomer returns grants for the customer, newest first.
// Empty customerID is rejected — list guests by order item (ListByOrderItem)
// until guest rebind lands.
func (s *Service) ListByCustomer(ctx context.Context, customerID string) ([]domain.DownloadGrant, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, apperror.Validation("download: customer id must not be empty")
	}
	rows, err := s.grants.ListByCustomerID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("download: list by customer: %w", err)
	}
	if rows == nil {
		rows = []domain.DownloadGrant{}
	}
	return rows, nil
}

// ListByOrderItem returns grants for the order line, newest first.
func (s *Service) ListByOrderItem(ctx context.Context, orderItemID string) ([]domain.DownloadGrant, error) {
	orderItemID = strings.TrimSpace(orderItemID)
	if canon, ok := domain.CanonicalOrderItemID(orderItemID); ok {
		orderItemID = canon
	}
	if orderItemID == "" {
		return nil, apperror.Validation("order item id must not be empty")
	}
	rows, err := s.grants.ListByOrderItemID(ctx, orderItemID)
	if err != nil {
		return nil, fmt.Errorf("download: list by order item: %w", err)
	}
	if rows == nil {
		rows = []domain.DownloadGrant{}
	}
	return rows, nil
}

// RecordDownload increments download_count after CanDownload succeeds.
// Uses CAS on the previous count so concurrent deliveries conflict cleanly.
// ErrExpired / ErrExhausted / ErrCountConflict remain detectable via errors.Is.
func (s *Service) RecordDownload(ctx context.Context, grantID string) (*domain.DownloadGrant, error) {
	g, err := s.Get(ctx, grantID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	expected := g.DownloadCount
	if err := g.RecordDownload(now); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, err.Error(), err)
	}
	if err := s.grants.UpdateCount(ctx, g.ID, g.DownloadCount, expected); err != nil {
		return nil, mapWriteError("record download", err)
	}
	return g, nil
}

// Extend updates expires_at (support re-issue path for PR-1078).
func (s *Service) Extend(ctx context.Context, grantID string, expiresAt time.Time) (*domain.DownloadGrant, error) {
	g, err := s.Get(ctx, grantID)
	if err != nil {
		return nil, err
	}
	if err := g.Extend(expiresAt); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.grants.Update(ctx, g); err != nil {
		return nil, mapWriteError("extend", err)
	}
	return g, nil
}

// Revoke ends the grant immediately (expires_at = now). Idempotent when
// already expired. Publishes download.grant.revoked. Admin HTTP remains PR-1078.
func (s *Service) Revoke(ctx context.Context, grantID, reason string) (*domain.DownloadGrant, bool, error) {
	g, idempotent, err := s.revoke(ctx, grantID)
	if err != nil {
		return nil, false, err
	}
	if !idempotent {
		s.publishRevoked(ctx, g, reason)
	}
	return g, idempotent, nil
}

func (s *Service) revoke(ctx context.Context, grantID string) (*domain.DownloadGrant, bool, error) {
	g, err := s.Get(ctx, grantID)
	if err != nil {
		return nil, false, err
	}
	now := s.now()
	if g.Expired(now) {
		return g, true, nil
	}
	if err := g.Revoke(now); err != nil {
		return nil, false, apperror.Validation(err.Error())
	}
	if err := s.grants.Update(ctx, g); err != nil {
		return nil, false, mapWriteError("revoke", err)
	}
	return g, false, nil
}

// RevokeForOrderItem revokes every grant on the order line (returns / refund).
// Does not publish — the caller (returns.Service) publishes after commit.
// Missing lines are a no-op. Returns the rows that were newly revoked.
func (s *Service) RevokeForOrderItem(ctx context.Context, orderItemID, reason string) ([]*domain.DownloadGrant, error) {
	_ = reason // caller publishes; kept for audit context parity with assignments
	rows, err := s.ListByOrderItem(ctx, orderItemID)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.DownloadGrant, 0, len(rows))
	for i := range rows {
		g, idempotent, err := s.revoke(ctx, rows[i].ID)
		if err != nil {
			return nil, err
		}
		if !idempotent && g != nil {
			out = append(out, g)
		}
	}
	return out, nil
}

// ResetCount clears download_count for a support re-issue via UpdateCount
// (Update does not write download_count).
func (s *Service) ResetCount(ctx context.Context, grantID string) (*domain.DownloadGrant, error) {
	g, err := s.Get(ctx, grantID)
	if err != nil {
		return nil, err
	}
	expected := g.DownloadCount
	if err := g.ResetCount(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.grants.UpdateCount(ctx, g.ID, g.DownloadCount, expected); err != nil {
		return nil, mapWriteError("reset count", err)
	}
	return g, nil
}

// CountByAssetID reports how many live grants reference the asset (media delete).
func (s *Service) CountByAssetID(ctx context.Context, assetID string) (int, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return 0, apperror.Validation("asset id must not be empty")
	}
	return s.grants.CountByAssetID(ctx, assetID)
}

// DeleteExpiredOrRevokedByAssetID removes non-live grant rows so media.Delete
// is not blocked by ON DELETE RESTRICT after the live-only count check.
func (s *Service) DeleteExpiredOrRevokedByAssetID(ctx context.Context, assetID string) (int, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return 0, apperror.Validation("asset id must not be empty")
	}
	return s.grants.DeleteExpiredOrRevokedByAssetID(ctx, assetID)
}

func (s *Service) publishIssued(ctx context.Context, g *domain.DownloadGrant, orderID string) {
	if s == nil || s.bus == nil || g == nil {
		return
	}
	_ = s.bus.Publish(ctx, event.New(domain.EventDownloadGrantIssued, "download.service", domain.GrantIssuedData{
		GrantID:     g.ID,
		OrderItemID: g.OrderItemID,
		CustomerID:  g.CustomerID,
		AssetID:     g.AssetID,
		OrderID:     orderID,
	}))
}

func (s *Service) publishRevoked(ctx context.Context, g *domain.DownloadGrant, reason string) {
	if s == nil || s.bus == nil || g == nil {
		return
	}
	_ = s.bus.Publish(ctx, event.New(domain.EventDownloadGrantRevoked, "download.service", domain.GrantRevokedData{
		GrantID:     g.ID,
		OrderItemID: g.OrderItemID,
		CustomerID:  g.CustomerID,
		AssetID:     g.AssetID,
		Reason:      strings.TrimSpace(reason),
	}))
}

func mapWriteError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrCountConflict) {
		return apperror.Wrap(apperror.CodeConflict, "download grant count conflict", err)
	}
	if apperror.Is(err, apperror.CodeConflict) || apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
		return err
	}
	return fmt.Errorf("download: %s: %w", op, err)
}
