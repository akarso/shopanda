package voucher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	domain "github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// txVoucherRepo is implemented by the postgres adapter.
type txVoucherRepo interface {
	domain.Repository
	WithTx(tx *sql.Tx) domain.Repository
}

type txRedemptionRepo interface {
	domain.RedemptionRepository
	WithTx(tx *sql.Tx) domain.RedemptionRepository
}

// Service is the application API for the voucher register and redeem flow.
type Service struct {
	vouchers    domain.Repository
	redemptions domain.RedemptionRepository
	settings    domain.SettingsRepository
	returns     blockingReturnChecker
	bus         *event.Bus
	log         logger.Logger
	txStarter   interface {
		BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	}
	now func() time.Time
}

// NewService constructs a voucher Service. redemptions may be nil until
// redeem/unredeem are wired; settings may be nil for read-only tests.
func NewService(vouchers domain.Repository, bus *event.Bus, log logger.Logger) *Service {
	if vouchers == nil {
		panic("voucher.NewService: nil vouchers")
	}
	return &Service{
		vouchers: vouchers,
		bus:      bus,
		log:      log,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// WithRedemptions wires the redemption audit store.
func (s *Service) WithRedemptions(r domain.RedemptionRepository) *Service {
	if s == nil {
		return nil
	}
	s.redemptions = r
	return s
}

// WithSettings wires product voucher settings.
func (s *Service) WithSettings(r domain.SettingsRepository) *Service {
	if s == nil {
		return nil
	}
	s.settings = r
	return s
}

// WithTxStarter enables transactional redeem/unredeem + audit insert.
func (s *Service) WithTxStarter(starter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}) *Service {
	if s == nil {
		return nil
	}
	s.txStarter = starter
	return s
}

// WithBlockingReturnChecker refuses redeem while the line is on an open
// or refunded return (same gate as issuance).
func (s *Service) WithBlockingReturnChecker(c blockingReturnChecker) *Service {
	if s == nil {
		return nil
	}
	s.returns = c
	return s
}

// LinkerWithTx returns a LinkCustomer binder scoped to tx (guest claim).
func (s *Service) LinkerWithTx(tx *sql.Tx) interface {
	LinkCustomer(ctx context.Context, orderID, customerID string) error
} {
	return s.WithTx(tx)
}

func (s *Service) withRepos(v domain.Repository, r domain.RedemptionRepository) *Service {
	cp := *s
	cp.vouchers = v
	if r != nil {
		cp.redemptions = r
	}
	return &cp
}

// GetSettings returns voucher settings for a product, or (nil, nil).
func (s *Service) GetSettings(ctx context.Context, productID string) (*domain.Settings, error) {
	if s == nil || s.settings == nil {
		return nil, apperror.Internal("voucher: settings repository not configured")
	}
	return s.settings.FindByProductID(ctx, productID)
}

// UpsertSettings writes voucher settings for a product.
func (s *Service) UpsertSettings(ctx context.Context, settings *domain.Settings) error {
	if s == nil || s.settings == nil {
		return apperror.Internal("voucher: settings repository not configured")
	}
	if settings == nil {
		return apperror.Validation("voucher settings must not be nil")
	}
	if err := settings.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	return s.settings.Upsert(ctx, settings)
}

// RemoveSettings deletes voucher settings for a product.
func (s *Service) RemoveSettings(ctx context.Context, productID string) error {
	if s == nil || s.settings == nil {
		return apperror.Internal("voucher: settings repository not configured")
	}
	return s.settings.Remove(ctx, productID)
}

// FindByID returns a voucher or NotFound.
func (s *Service) FindByID(ctx context.Context, voucherID string) (*domain.Voucher, error) {
	v, err := s.vouchers.FindByID(ctx, voucherID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, apperror.NotFound("voucher not found")
	}
	return v, nil
}

// FindByCode looks up a voucher by code (dashed or normalized).
func (s *Service) FindByCode(ctx context.Context, code string) (*domain.Voucher, error) {
	v, err := s.vouchers.FindByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, apperror.NotFound("voucher not found")
	}
	return v, nil
}

// List returns vouchers matching the filter.
func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.Voucher, error) {
	return s.vouchers.List(ctx, f)
}

// ListByOrder returns vouchers for an order.
func (s *Service) ListByOrder(ctx context.Context, orderID string) ([]domain.Voucher, error) {
	return s.vouchers.ListByOrderID(ctx, orderID)
}

// ListRedemptions returns audit rows for a voucher.
func (s *Service) ListRedemptions(ctx context.Context, voucherID string) ([]domain.Redemption, error) {
	if s.redemptions == nil {
		return nil, apperror.Internal("voucher: redemptions repository not configured")
	}
	return s.redemptions.ListByVoucherID(ctx, voucherID)
}

// Redeem increments redeemed_count and writes an audit row.
func (s *Service) Redeem(ctx context.Context, voucherID, actorID, note string) (*domain.Voucher, error) {
	return s.mutateRedeem(ctx, voucherID, actorID, note, domain.ActionRedeem)
}

// Unredeem decrements redeemed_count and writes an audit row.
// note is required (staff reason), matching download revoke discipline.
func (s *Service) Unredeem(ctx context.Context, voucherID, actorID, note string) (*domain.Voucher, error) {
	if strings.TrimSpace(note) == "" {
		return nil, apperror.Validation("unredeem requires a note")
	}
	return s.mutateRedeem(ctx, voucherID, actorID, note, domain.ActionUnredeem)
}

// VoidForOrderItem voids live vouchers for a returned order line.
func (s *Service) VoidForOrderItem(ctx context.Context, orderItemID string) ([]domain.Voucher, error) {
	return s.vouchers.VoidForOrderItem(ctx, orderItemID, s.now())
}

// LinkCustomer stamps customer_id onto guest vouchers for the order.
func (s *Service) LinkCustomer(ctx context.Context, orderID, customerID string) error {
	return s.vouchers.LinkCustomer(ctx, orderID, customerID)
}

// BindsTx reports whether the voucher repo supports WithTx.
func (s *Service) BindsTx() bool {
	_, ok := s.vouchers.(txVoucherRepo)
	return ok
}

// WithTx returns a Service bound to tx (returns/approve path).
func (s *Service) WithTx(tx *sql.Tx) *Service {
	txRepo, ok := s.vouchers.(txVoucherRepo)
	if !ok {
		return s
	}
	var red domain.RedemptionRepository
	if txRed, okR := s.redemptions.(txRedemptionRepo); okR {
		red = txRed.WithTx(tx)
	}
	return s.withRepos(txRepo.WithTx(tx), red)
}

func (s *Service) mutateRedeem(ctx context.Context, voucherID, actorID, note, action string) (*domain.Voucher, error) {
	voucherID = strings.TrimSpace(voucherID)
	actorID = strings.TrimSpace(actorID)
	note = strings.TrimSpace(note)
	if voucherID == "" {
		return nil, apperror.Validation("voucher id must not be empty")
	}
	if actorID == "" {
		return nil, apperror.Validation("actor id must not be empty")
	}
	if len(note) > domain.MaxNoteLen {
		return nil, apperror.Validation("note exceeds maximum length")
	}

	run := func(svc *Service, tx *sql.Tx) (*domain.Voucher, error) {
		v, err := svc.vouchers.FindByID(ctx, voucherID)
		if err != nil {
			return nil, err
		}
		if v == nil {
			return nil, apperror.NotFound("voucher not found")
		}
		// Serialize against return void / IssueForOrder on the same unit.
		if tx != nil {
			if err := assignmentapp.LockAssignmentUnits(ctx, tx, v.OrderItemID); err != nil {
				return nil, err
			}
			// Re-read under the lock so redeemed_count / voided_at are current.
			v, err = svc.vouchers.FindByID(ctx, voucherID)
			if err != nil {
				return nil, err
			}
			if v == nil {
				return nil, apperror.NotFound("voucher not found")
			}
		}
		expected := v.RedeemedCount
		var next int
		switch action {
		case domain.ActionRedeem, domain.ActionUnredeem:
			if action == domain.ActionRedeem {
				if err := v.CanRedeem(); err != nil {
					return nil, mapRedeemErr(err)
				}
			} else {
				if err := v.CanUnredeem(); err != nil {
					return nil, mapRedeemErr(err)
				}
			}
			// Both redeem and unredeem refuse on a blocking return so staff
			// cannot clear redeemed_count and then approve/refund the line.
			if svc.returns != nil {
				blocked, err := svc.returns.UnitsOnBlockingReturn(ctx, []string{v.OrderItemID})
				if err != nil {
					return nil, err
				}
				if _, ok := blocked[v.OrderItemID]; ok {
					return nil, mapRedeemErr(domain.ErrOnBlockingReturn)
				}
			}
			if action == domain.ActionRedeem {
				next = expected + 1
			} else {
				next = expected - 1
			}
		default:
			return nil, apperror.Validation("invalid redeem action")
		}
		if err := svc.vouchers.UpdateCounts(ctx, v.ID, next, expected); err != nil {
			return nil, err
		}
		if svc.redemptions != nil {
			row, err := domain.NewRedemption(id.New(), v.ID, action, actorID, note, svc.now())
			if err != nil {
				return nil, apperror.Validation(err.Error())
			}
			if err := svc.redemptions.Insert(ctx, row); err != nil {
				return nil, err
			}
		}
		v.RedeemedCount = next
		return v, nil
	}

	txRepo, okV := s.vouchers.(txVoucherRepo)
	if okV && s.txStarter != nil {
		tx, err := s.txStarter.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("voucher: begin tx: %w", err)
		}
		defer tx.Rollback()
		var red domain.RedemptionRepository
		if txRed, okR := s.redemptions.(txRedemptionRepo); okR {
			red = txRed.WithTx(tx)
		}
		bound := s.withRepos(txRepo.WithTx(tx), red)
		v, err := run(bound, tx)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("voucher: commit redeem: %w", err)
		}
		s.publishRedeemed(ctx, v, actorID, action)
		return v, nil
	}
	v, err := run(s, nil)
	if err != nil {
		return nil, err
	}
	s.publishRedeemed(ctx, v, actorID, action)
	return v, nil
}

func mapRedeemErr(err error) error {
	switch {
	case errors.Is(err, domain.ErrVoid):
		return apperror.Validation("voucher is void")
	case errors.Is(err, domain.ErrExhausted):
		return apperror.Validation("voucher is fully redeemed")
	case errors.Is(err, domain.ErrNothingToUnredeem):
		return apperror.Validation("voucher has no redemptions to undo")
	case errors.Is(err, domain.ErrOnBlockingReturn):
		return apperror.Validation("voucher cannot be redeemed or unredeemed while the order line is on a return")
	default:
		return apperror.Validation(err.Error())
	}
}

func (s *Service) publishRedeemed(ctx context.Context, v *domain.Voucher, actorID, action string) {
	if s == nil || s.bus == nil || v == nil {
		return
	}
	evtName := domain.EventVoucherRedeemed
	if action == domain.ActionUnredeem {
		evtName = domain.EventVoucherUnredeemed
	}
	s.bus.Publish(ctx, event.New(evtName, "core.voucher", domain.RedeemedData{
		VoucherID: v.ID,
		OrderID:   v.OrderID,
		Code:      domain.FormatCode(v.Code),
		ActorID:   actorID,
		Action:    action,
	}))
}

// PublishIssued publishes voucher.issued after a successful issuance commit.
func (s *Service) PublishIssued(ctx context.Context, orderID, customerID string, voucherIDs []string) {
	if s == nil || s.bus == nil || len(voucherIDs) == 0 {
		return
	}
	s.bus.Publish(ctx, event.New(domain.EventVoucherIssued, "core.voucher", domain.IssuedData{
		OrderID:    orderID,
		CustomerID: customerID,
		VoucherIDs: voucherIDs,
	}))
}
