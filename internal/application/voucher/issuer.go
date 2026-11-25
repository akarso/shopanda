package voucher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	domain "github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const maxCodeAttempts = 8

type orderFinder interface {
	FindByID(ctx context.Context, id string) (*order.Order, error)
}

type paymentFinder interface {
	FindByOrderID(ctx context.Context, orderID string) (*payment.Payment, error)
}

type variantFinder interface {
	FindByID(ctx context.Context, id string) (*catalog.Variant, error)
}

type productFinder interface {
	FindByID(ctx context.Context, id string) (*catalog.Product, error)
}

type settingsFinder interface {
	FindByProductID(ctx context.Context, productID string) (*domain.Settings, error)
}

type blockingReturnChecker interface {
	UnitsOnBlockingReturn(ctx context.Context, unitIDs []string) (map[string]struct{}, error)
}

type txStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Issuer creates voucher codes when order.paid fires for virtual lines
// with checkout-snapshotted entitlement (or live settings for legacy rows).
type Issuer struct {
	orders   orderFinder
	payments paymentFinder
	variants variantFinder
	products productFinder
	settings settingsFinder
	returns  blockingReturnChecker
	svc      *Service
	log      logger.Logger
	txStart  txStarter
	now      func() time.Time
}

// NewIssuer constructs an order.paid voucher issuer.
func NewIssuer(
	orders orderFinder,
	variants variantFinder,
	products productFinder,
	settings settingsFinder,
	svc *Service,
	log logger.Logger,
) *Issuer {
	if orders == nil {
		panic("voucher.NewIssuer: nil orders")
	}
	if variants == nil {
		panic("voucher.NewIssuer: nil variants")
	}
	if products == nil {
		panic("voucher.NewIssuer: nil products")
	}
	if settings == nil {
		panic("voucher.NewIssuer: nil settings")
	}
	if svc == nil {
		panic("voucher.NewIssuer: nil service")
	}
	return &Issuer{
		orders:   orders,
		variants: variants,
		products: products,
		settings: settings,
		svc:      svc,
		log:      log,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// WithPayments wires payment lookup for the unpaid refuse check.
func (i *Issuer) WithPayments(payments paymentFinder) *Issuer {
	if i != nil {
		i.payments = payments
	}
	return i
}

// WithTxStarter wraps inserts for one order in a transaction.
func (i *Issuer) WithTxStarter(starter txStarter) *Issuer {
	if i != nil {
		i.txStart = starter
	}
	return i
}

// WithBlockingReturnChecker skips lines on open/refunded returns.
func (i *Issuer) WithBlockingReturnChecker(c blockingReturnChecker) *Issuer {
	if i != nil {
		i.returns = c
	}
	return i
}

// HandleOrderPaid is the bus handler for order.EventOrderPaid.
func (i *Issuer) HandleOrderPaid(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(order.OrderStatusChangedData)
	if !ok {
		return fmt.Errorf("voucher: unexpected event data type %T", evt.Data)
	}
	orderID := strings.TrimSpace(data.OrderID)
	if orderID == "" {
		return fmt.Errorf("voucher: order.paid missing order id")
	}
	_, err := i.IssueForOrder(ctx, orderID)
	return err
}

// IssueForOrder creates codes for every voucher-entitled line.
// Idempotent on (order_id, variant_id, seq). Returns newly inserted count.
// Takes the same per-unit advisory locks as return void, then rechecks
// blocking returns inside the transaction so a concurrent refund cannot
// leave live codes on a voided line.
func (i *Issuer) IssueForOrder(ctx context.Context, orderID string) (int, error) {
	orderID = strings.TrimSpace(orderID)
	o, err := i.orders.FindByID(ctx, orderID)
	if err != nil {
		return 0, fmt.Errorf("voucher: find order %s: %w", orderID, err)
	}
	if o == nil {
		return 0, fmt.Errorf("voucher: order %s not found", orderID)
	}
	if err := i.assertOrderPaid(ctx, o); err != nil {
		return 0, err
	}

	txRepo, bindsTx := i.svc.vouchers.(txVoucherRepo)
	if bindsTx {
		if i.txStart == nil {
			return 0, apperror.Internal("voucher: transaction starter is required for issuance")
		}
		tx, err := i.txStart.BeginTx(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("voucher: begin tx: %w", err)
		}
		defer tx.Rollback()
		unitIDs := make([]string, 0, len(o.Items()))
		for _, it := range o.Items() {
			unitIDs = append(unitIDs, domain.OrderItemKey(o.ID, it.VariantID))
		}
		if err := assignmentapp.LockAssignmentUnits(ctx, tx, unitIDs...); err != nil {
			return 0, err
		}
		plans, err := i.planLines(ctx, o)
		if err != nil {
			return 0, err
		}
		if len(plans) == 0 {
			return 0, nil
		}
		bound := i.svc.withRepos(txRepo.WithTx(tx), nil)
		created, err := i.insertPlans(ctx, bound, o, plans, tx)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("voucher: commit: %w", err)
		}
		ids := make([]string, len(created))
		for i, v := range created {
			ids[i] = v.ID
		}
		i.svc.PublishIssued(ctx, o.ID, o.CustomerID, ids)
		return len(created), nil
	}

	plans, err := i.planLines(ctx, o)
	if err != nil {
		return 0, err
	}
	if len(plans) == 0 {
		return 0, nil
	}
	created, err := i.insertPlans(ctx, i.svc, o, plans, nil)
	if err != nil {
		return 0, err
	}
	ids := make([]string, len(created))
	for i, v := range created {
		ids[i] = v.ID
	}
	i.svc.PublishIssued(ctx, o.ID, o.CustomerID, ids)
	return len(created), nil
}

// PublishExistingIssued republishes voucher.issued for every live code on the
// order (admin resend / reissue-with-nothing-created path).
func (i *Issuer) PublishExistingIssued(ctx context.Context, orderID string) error {
	rows, err := i.svc.ListByOrder(ctx, orderID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(rows))
	customerID := ""
	for i := range rows {
		if rows[i].VoidedAt != nil {
			continue
		}
		ids = append(ids, rows[i].ID)
		if customerID == "" {
			customerID = rows[i].CustomerID
		}
	}
	if len(ids) == 0 {
		return apperror.NotFound("no vouchers for order")
	}
	i.svc.PublishIssued(ctx, orderID, customerID, ids)
	return nil
}

type issuePlan struct {
	variantID   string
	productID   string
	orderItemID string
	seq         int
	maxRedeem   int
}

func (i *Issuer) planLines(ctx context.Context, o *order.Order) ([]issuePlan, error) {
	items := o.Items()
	unitIDs := make([]string, 0, len(items))
	for _, it := range items {
		unitIDs = append(unitIDs, domain.OrderItemKey(o.ID, it.VariantID))
	}
	blocked := map[string]struct{}{}
	if i.returns != nil && len(unitIDs) > 0 {
		var err error
		blocked, err = i.returns.UnitsOnBlockingReturn(ctx, unitIDs)
		if err != nil {
			return nil, fmt.Errorf("voucher: blocking returns: %w", err)
		}
	}

	var plans []issuePlan
	for _, it := range items {
		key := domain.OrderItemKey(o.ID, it.VariantID)
		if _, skip := blocked[key]; skip {
			continue
		}
		codesPerUnit, maxRedeem, ok, err := i.entitlementForLine(ctx, it)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		n := codesPerUnit * it.Quantity
		if n > domain.MaxCodesPerUnit*100 {
			return nil, apperror.Validation(fmt.Sprintf("voucher: issuing %d codes for variant %s exceeds line cap", n, it.VariantID))
		}
		variant, err := i.variants.FindByID(ctx, it.VariantID)
		if err != nil {
			return nil, fmt.Errorf("voucher: find variant %s: %w", it.VariantID, err)
		}
		if variant == nil {
			return nil, fmt.Errorf("voucher: variant %s not found", it.VariantID)
		}
		for seq := 0; seq < n; seq++ {
			plans = append(plans, issuePlan{
				variantID:   it.VariantID,
				productID:   variant.ProductID,
				orderItemID: key,
				seq:         seq,
				maxRedeem:   maxRedeem,
			})
		}
	}
	return plans, nil
}

// entitlementForLine prefers checkout snapshot; falls back to live settings
// for legacy rows created before migration 116.
func (i *Issuer) entitlementForLine(ctx context.Context, it order.Item) (codesPerUnit, maxRedeem int, ok bool, err error) {
	if it.VoucherCodesPerUnit != nil && it.VoucherMaxRedemptions != nil {
		return *it.VoucherCodesPerUnit, *it.VoucherMaxRedemptions, true, nil
	}
	variant, err := i.variants.FindByID(ctx, it.VariantID)
	if err != nil {
		return 0, 0, false, fmt.Errorf("voucher: find variant %s: %w", it.VariantID, err)
	}
	if variant == nil {
		return 0, 0, false, fmt.Errorf("voucher: variant %s not found", it.VariantID)
	}
	product, err := i.products.FindByID(ctx, variant.ProductID)
	if err != nil {
		return 0, 0, false, fmt.Errorf("voucher: find product %s: %w", variant.ProductID, err)
	}
	if product == nil || product.Type != catalog.TypeVirtual {
		return 0, 0, false, nil
	}
	settings, err := i.settings.FindByProductID(ctx, product.ID)
	if err != nil {
		return 0, 0, false, fmt.Errorf("voucher: settings %s: %w", product.ID, err)
	}
	if settings == nil || !settings.Enabled {
		return 0, 0, false, nil
	}
	if err := settings.Validate(); err != nil {
		return 0, 0, false, apperror.Validation(err.Error())
	}
	return settings.CodesPerUnit, settings.MaxRedemptions, true, nil
}

func (i *Issuer) insertPlans(ctx context.Context, svc *Service, o *order.Order, plans []issuePlan, tx *sql.Tx) ([]*domain.Voucher, error) {
	created := make([]*domain.Voucher, 0, len(plans))
	now := i.now()
	for _, p := range plans {
		v, err := i.insertOne(ctx, svc, o, p, now, tx)
		if err != nil {
			return nil, err
		}
		if v != nil {
			created = append(created, v)
		}
	}
	return created, nil
}

func (i *Issuer) insertOne(ctx context.Context, svc *Service, o *order.Order, p issuePlan, now time.Time, tx *sql.Tx) (*domain.Voucher, error) {
	for attempt := 0; attempt < maxCodeAttempts; attempt++ {
		code, err := domain.GenerateCode()
		if err != nil {
			return nil, err
		}
		v, err := domain.NewVoucher(
			id.New(), code, o.ID, p.orderItemID, p.productID, p.variantID, o.CustomerID,
			p.seq, p.maxRedeem, now,
		)
		if err != nil {
			return nil, apperror.Validation(err.Error())
		}
		if tx != nil {
			if _, err := tx.ExecContext(ctx, "SAVEPOINT voucher_insert"); err != nil {
				return nil, fmt.Errorf("voucher: savepoint: %w", err)
			}
		}
		err = svc.vouchers.Insert(ctx, v)
		if err == nil {
			if tx != nil {
				if _, relErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT voucher_insert"); relErr != nil {
					return nil, fmt.Errorf("voucher: release savepoint: %w", relErr)
				}
			}
			return v, nil
		}
		if errors.Is(err, domain.ErrAlreadyIssued) {
			if tx != nil {
				if _, rbErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT voucher_insert"); rbErr != nil {
					return nil, fmt.Errorf("voucher: rollback savepoint: %w", rbErr)
				}
				if _, relErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT voucher_insert"); relErr != nil {
					return nil, fmt.Errorf("voucher: release savepoint: %w", relErr)
				}
			}
			return nil, nil // already issued
		}
		if errors.Is(err, domain.ErrCodeTaken) {
			if tx != nil {
				if _, rbErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT voucher_insert"); rbErr != nil {
					return nil, fmt.Errorf("voucher: rollback savepoint: %w", rbErr)
				}
				if _, relErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT voucher_insert"); relErr != nil {
					return nil, fmt.Errorf("voucher: release savepoint: %w", relErr)
				}
			}
			continue
		}
		if tx != nil {
			if _, rbErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT voucher_insert"); rbErr != nil {
				return nil, fmt.Errorf("voucher: rollback savepoint: %w", rbErr)
			}
			if _, relErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT voucher_insert"); relErr != nil {
				return nil, fmt.Errorf("voucher: release savepoint: %w", relErr)
			}
		}
		return nil, err
	}
	return nil, apperror.Internal("voucher: exhausted code generation retries")
}

func (i *Issuer) assertOrderPaid(ctx context.Context, o *order.Order) error {
	if o == nil {
		return apperror.Validation("voucher: order must not be nil")
	}
	if o.Status() == order.OrderStatusCancelled || o.Status() == order.OrderStatusFailed {
		return apperror.Validation("voucher: order is not paid")
	}
	if o.Status() == order.OrderStatusPaid {
		return nil
	}
	var py *payment.Payment
	if i.payments != nil {
		var err error
		py, err = i.payments.FindByOrderID(ctx, o.ID)
		if err != nil {
			return fmt.Errorf("voucher: find payment for order %s: %w", o.ID, err)
		}
	}
	if py != nil {
		if py.Status() == payment.StatusCompleted {
			return nil
		}
		return apperror.Validation("voucher: order is not paid")
	}
	payable, err := o.PayableAmount()
	if err != nil {
		return fmt.Errorf("voucher: payable amount: %w", err)
	}
	if payable.Amount() == 0 {
		return nil
	}
	return apperror.Validation("voucher: order is not paid")
}
