package download

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// orderFinder loads an order for grant issuance.
type orderFinder interface {
	FindByID(ctx context.Context, id string) (*order.Order, error)
}

// paymentFinder loads the payment for unpaid checks (same ownership rule
// as assignment.Flow: completed payment, or payable 0 with no payment row).
type paymentFinder interface {
	FindByOrderID(ctx context.Context, orderID string) (*payment.Payment, error)
}

// variantFinder resolves a line's variant to its product.
type variantFinder interface {
	FindByID(ctx context.Context, id string) (*catalog.Variant, error)
}

// productFinder loads the product type for a variant's product.
type productFinder interface {
	FindByID(ctx context.Context, id string) (*catalog.Product, error)
}

// fileLister lists downloadable_files attachments for a product.
type fileLister interface {
	ListByProductID(ctx context.Context, productID string) ([]catalog.DownloadableFile, error)
}

// settingsFinder loads optional per-product grant overrides.
type settingsFinder interface {
	FindByProductID(ctx context.Context, productID string) (*domain.GrantSettings, error)
}

// componentLister lists bundle components for non-exploded downloadable leaves.
type componentLister interface {
	ListByBundleID(ctx context.Context, bundleProductID string) ([]catalog.BundleComponent, error)
}

// txStarter begins the transaction that wraps all grants for one order.
type txStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// txGrantRepo binds the grant repository to a transaction.
type txGrantRepo interface {
	domain.Repository
	WithTx(tx *sql.Tx) domain.Repository
}

// blockingReturnChecker reports order lines still blocked by an open or
// refunded return (same gate as assignment Track D). Used so a late
// order.paid heal does not issue live grants for returned lines.
type blockingReturnChecker interface {
	UnitsOnBlockingReturn(ctx context.Context, unitIDs []string) (map[string]struct{}, error)
}

// Issuer creates DownloadGrant rows when order.paid fires for lines whose
// product type is downloadable (PR-1075). It is subscribed on the event
// bus — not inlined in payment capture. Delivery that consumes the grant
// is PR-1077.
type Issuer struct {
	orders     orderFinder
	payments   paymentFinder
	variants   variantFinder
	products   productFinder
	files      fileLister
	settings   settingsFinder
	components componentLister
	returns    blockingReturnChecker
	grants     *Service
	defaults   domain.Defaults
	log        logger.Logger
	txStarter  txStarter
	now        func() time.Time
}

// NewIssuer constructs an order.paid grant issuer. settings may be nil
// (always use global defaults). payments may be nil only in tests that
// pre-mark orders paid; production always wires the payment repo. log may
// be nil.
func NewIssuer(
	orders orderFinder,
	variants variantFinder,
	products productFinder,
	files fileLister,
	settings settingsFinder,
	grants *Service,
	defaults domain.Defaults,
	log logger.Logger,
) *Issuer {
	if orders == nil {
		panic("download.NewIssuer: nil orders")
	}
	if variants == nil {
		panic("download.NewIssuer: nil variants")
	}
	if products == nil {
		panic("download.NewIssuer: nil products")
	}
	if files == nil {
		panic("download.NewIssuer: nil files")
	}
	if grants == nil {
		panic("download.NewIssuer: nil grants service")
	}
	if err := domain.ValidateDefaults(defaults); err != nil {
		panic("download.NewIssuer: " + err.Error())
	}
	return &Issuer{
		orders:   orders,
		variants: variants,
		products: products,
		files:    files,
		settings: settings,
		grants:   grants,
		defaults: defaults,
		log:      log,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// WithPayments wires payment lookup for the unpaid refuse check.
func (i *Issuer) WithPayments(payments paymentFinder) *Issuer {
	if i == nil {
		return nil
	}
	i.payments = payments
	return i
}

// WithBundleComponents walks required downloadable leaves on non-exploded
// bundle parents (same one-level required walk as cart/checkout). Nil
// fails closed when a paid order contains a bundle line.
func (i *Issuer) WithBundleComponents(components componentLister) *Issuer {
	if i == nil {
		return nil
	}
	i.components = components
	return i
}

// WithTxStarter wraps all grant inserts for one order in a transaction.
// Without it, IssueForOrder fails closed when the grant repo supports WithTx.
func (i *Issuer) WithTxStarter(starter txStarter) *Issuer {
	if i == nil {
		return nil
	}
	i.txStarter = starter
	return i
}

// WithBlockingReturnChecker skips issuance for lines on an open or refunded
// return so webhook/admin order.paid republish cannot create live grants
// after approve/refund revoke (or when revoke was a no-op because issuance
// had not yet succeeded). Nil skips the check (tests).
func (i *Issuer) WithBlockingReturnChecker(c blockingReturnChecker) *Issuer {
	if i == nil {
		return nil
	}
	i.returns = c
	return i
}

// HandleOrderPaid is an event handler for order.EventOrderPaid. It issues
// one grant per attached asset on each downloadable line (and required
// downloadable leaves of non-exploded bundles). Re-delivery is idempotent
// via (order_item_id, asset_id). Failures return so the bus can log them.
func (i *Issuer) HandleOrderPaid(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(order.OrderStatusChangedData)
	if !ok {
		return fmt.Errorf("download: unexpected event data type %T", evt.Data)
	}
	orderID := strings.TrimSpace(data.OrderID)
	if orderID == "" {
		return fmt.Errorf("download: order.paid missing order id")
	}
	_, err := i.IssueForOrder(ctx, orderID)
	return err
}

// IssueForOrder loads the order and creates grants for every downloadable
// line (and required downloadable bundle leaves). Returns how many grants
// were newly inserted (idempotent hits are not counted). Refuses unpaid
// orders unless payable is 0 and payment was skipped (no payment row). A
// downloadable line with no files or a missing variant/product fails the
// handler. grant.issued is published only after a successful Commit.
func (i *Issuer) IssueForOrder(ctx context.Context, orderID string) (int, error) {
	orderID = strings.TrimSpace(orderID)
	o, err := i.orders.FindByID(ctx, orderID)
	if err != nil {
		return 0, fmt.Errorf("download: find order %s: %w", orderID, err)
	}
	if o == nil {
		return 0, fmt.Errorf("download: order %s not found", orderID)
	}
	if err := i.assertOrderPaid(ctx, o); err != nil {
		return 0, err
	}

	txRepo, bindsTx := i.grants.grants.(txGrantRepo)
	if bindsTx {
		if i.txStarter == nil {
			return 0, apperror.Internal("download: transaction starter is required for grant issuance")
		}
		tx, err := i.txStarter.BeginTx(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("download: begin tx: %w", err)
		}
		defer tx.Rollback()
		bound := i.grants.withRepo(txRepo.WithTx(tx))
		created, err := i.issueAllLines(ctx, bound, o)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("download: commit grants: %w", err)
		}
		i.publishIssuedGrants(ctx, created, o.ID)
		return len(created), nil
	}

	created, err := i.issueAllLines(ctx, i.grants, o)
	if err != nil {
		return 0, err
	}
	i.publishIssuedGrants(ctx, created, o.ID)
	return len(created), nil
}

func (i *Issuer) publishIssuedGrants(ctx context.Context, created []*domain.DownloadGrant, orderID string) {
	if i == nil || i.grants == nil {
		return
	}
	for _, g := range created {
		i.grants.publishIssued(ctx, g, orderID)
	}
}

func (i *Issuer) issueAllLines(ctx context.Context, grants *Service, o *order.Order) ([]*domain.DownloadGrant, error) {
	// Post-purchase reads must see draft/archived products so a later catalog
	// status change does not roll back the whole order's grants as "missing".
	ctx = catalog.WithIncludeNonActiveProducts(ctx)

	blocked := map[string]struct{}{}
	if i.returns != nil {
		unitIDs := make([]string, 0, len(o.Items()))
		for _, item := range o.Items() {
			unitIDs = append(unitIDs, domain.OrderItemID(o.ID, item.VariantID))
		}
		var err error
		blocked, err = i.returns.UnitsOnBlockingReturn(ctx, unitIDs)
		if err != nil {
			return nil, fmt.Errorf("download: blocking-return check: %w", err)
		}
	}

	created := make([]*domain.DownloadGrant, 0)
	for _, item := range o.Items() {
		orderItemID := domain.OrderItemID(o.ID, item.VariantID)
		if _, skip := blocked[orderItemID]; skip {
			continue
		}
		rows, err := i.issueForLine(ctx, grants, o, item)
		if err != nil {
			return created, err
		}
		created = append(created, rows...)
	}
	return created, nil
}

func (i *Issuer) assertOrderPaid(ctx context.Context, o *order.Order) error {
	if o == nil {
		return apperror.Validation("download: order must not be nil")
	}
	if o.Status() == order.OrderStatusCancelled || o.Status() == order.OrderStatusFailed {
		return apperror.Validation("download: order is not paid")
	}
	if o.Status() == order.OrderStatusPaid {
		return nil
	}
	var py *payment.Payment
	if i.payments != nil {
		var err error
		py, err = i.payments.FindByOrderID(ctx, o.ID)
		if err != nil {
			return fmt.Errorf("download: find payment for order %s: %w", o.ID, err)
		}
	}
	if py != nil {
		if py.Status() == payment.StatusCompleted {
			return nil
		}
		return apperror.Validation("download: order is not paid")
	}
	payable, err := o.PayableAmount()
	if err != nil {
		return fmt.Errorf("download: payable amount: %w", err)
	}
	if payable.Amount() == 0 {
		return nil
	}
	return apperror.Validation("download: order is not paid")
}

func (i *Issuer) issueForLine(ctx context.Context, grants *Service, o *order.Order, item order.Item) ([]*domain.DownloadGrant, error) {
	variant, err := i.variants.FindByID(ctx, item.VariantID)
	if err != nil {
		return nil, fmt.Errorf("download: find variant %s: %w", item.VariantID, err)
	}
	if variant == nil {
		return nil, apperror.Validation(fmt.Sprintf("download: variant %s missing for paid order %s", item.VariantID, o.ID))
	}
	product, err := i.products.FindByID(ctx, variant.ProductID)
	if err != nil {
		return nil, fmt.Errorf("download: find product %s: %w", variant.ProductID, err)
	}
	if product == nil {
		return nil, apperror.Validation(fmt.Sprintf("download: product %s missing for paid order %s", variant.ProductID, o.ID))
	}

	orderItemID := domain.OrderItemID(o.ID, item.VariantID)
	customerID := strings.TrimSpace(o.CustomerID)
	qty := item.Quantity

	switch product.Type {
	case catalog.TypeDownloadable:
		return i.issueForProduct(ctx, grants, o.ID, orderItemID, customerID, product.ID, qty)
	case catalog.TypeBundle:
		return i.issueForBundleLeaves(ctx, grants, o.ID, orderItemID, customerID, product.ID, qty)
	default:
		return nil, nil
	}
}

func (i *Issuer) issueForBundleLeaves(
	ctx context.Context,
	grants *Service,
	orderID, orderItemID, customerID, bundleProductID string,
	qty int,
) ([]*domain.DownloadGrant, error) {
	if i.components == nil {
		return nil, apperror.Internal("download: bundle components repository is required for bundle grant issuance")
	}
	rows, err := i.components.ListByBundleID(ctx, bundleProductID)
	if err != nil {
		return nil, fmt.Errorf("download: list bundle %s components: %w", bundleProductID, err)
	}
	created := make([]*domain.DownloadGrant, 0)
	for _, component := range rows {
		if !component.Required {
			continue
		}
		leaf, err := i.products.FindByID(ctx, component.ComponentProductID)
		if err != nil {
			return created, fmt.Errorf("download: find bundle leaf %s: %w", component.ComponentProductID, err)
		}
		if leaf == nil || leaf.Type != catalog.TypeDownloadable {
			continue
		}
		leafGrants, err := i.issueForProduct(ctx, grants, orderID, orderItemID, customerID, leaf.ID, qty*component.Quantity)
		if err != nil {
			return created, err
		}
		created = append(created, leafGrants...)
	}
	return created, nil
}

func (i *Issuer) issueForProduct(
	ctx context.Context,
	grants *Service,
	orderID, orderItemID, customerID, productID string,
	qty int,
) ([]*domain.DownloadGrant, error) {
	files, err := i.files.ListByProductID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("download: list files for product %s: %w", productID, err)
	}
	if len(files) == 0 {
		return nil, apperror.Validation(fmt.Sprintf("download: product %s has no attached files for paid order %s", productID, orderID))
	}

	var settings *domain.GrantSettings
	if i.settings != nil {
		settings, err = i.settings.FindByProductID(ctx, productID)
		if err != nil {
			return nil, fmt.Errorf("download: find grant settings for product %s: %w", productID, err)
		}
	}
	expiresIn, limit, err := domain.Resolve(settings, i.defaults)
	if err != nil {
		return nil, fmt.Errorf("download: resolve defaults for product %s: %w", productID, err)
	}
	if qty < 1 {
		qty = 1
	}
	totalLimit := limit * qty
	if totalLimit > domain.MaxDownloadLimit {
		totalLimit = domain.MaxDownloadLimit
	}
	expiresAt := i.now().Add(expiresIn)

	created := make([]*domain.DownloadGrant, 0, len(files))
	for _, f := range files {
		g, existed, err := grants.IssueIdempotent(ctx, orderItemID, customerID, f.AssetID, expiresAt, totalLimit)
		if err != nil {
			return created, fmt.Errorf("download: issue grant order=%s asset=%s: %w", orderID, f.AssetID, err)
		}
		if !existed {
			created = append(created, g)
		}
	}
	return created, nil
}
