package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
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

// errIssueLockSetChanged is returned when a required downloadable product
// appears after the initial lock pass (e.g. optional→required mid-issue).
// IssueForOrder rolls back and retries collect+lock from a fresh TX.
var errIssueLockSetChanged = errors.New("download: issuance lock set changed")

// maxIssueLockAttempts bounds collect→lock→recollect restarts when admin
// edits make the required leaf set grow under the locks.
const maxIssueLockAttempts = 5

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

// productTxBinder binds a product repository to the grant-issuance transaction
// so IssueForOrder can take the same products FOR UPDATE lock as Replace/Attach.
type productTxBinder interface {
	WithTx(tx *sql.Tx) catalog.ProductRepository
}

// lockingProductRepo locks a product row inside a transaction (SELECT FOR UPDATE).
type lockingProductRepo interface {
	LockForUpdate(ctx context.Context, id string) (catalog.Type, error)
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
		return i.issueForOrderTx(ctx, o, txRepo)
	}

	created, err := i.issueAllLines(ctx, i.grants, o, nil)
	if err != nil {
		return 0, err
	}
	i.publishIssuedGrants(ctx, created, o.ID)
	return len(created), nil
}

// issueForOrderTx runs collect→lock→recollect under a TX. If a new required
// leaf appears after the first lock pass, the TX is rolled back and the
// whole collect+lock cycle restarts (bounded).
func (i *Issuer) issueForOrderTx(ctx context.Context, o *order.Order, txRepo txGrantRepo) (int, error) {
	var lastErr error
	for attempt := 0; attempt < maxIssueLockAttempts; attempt++ {
		tx, err := i.txStarter.BeginTx(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("download: begin tx: %w", err)
		}
		bound := i.grants.withRepo(txRepo.WithTx(tx))
		created, err := i.issueAllLines(ctx, bound, o, tx)
		if errors.Is(err, errIssueLockSetChanged) {
			_ = tx.Rollback()
			lastErr = err
			continue
		}
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("download: commit grants: %w", err)
		}
		i.publishIssuedGrants(ctx, created, o.ID)
		return len(created), nil
	}
	return 0, fmt.Errorf("download: grant issuance lock set unstable after %d attempts: %w", maxIssueLockAttempts, lastErr)
}

func (i *Issuer) publishIssuedGrants(ctx context.Context, created []*domain.DownloadGrant, orderID string) {
	if i == nil || i.grants == nil {
		return
	}
	for _, g := range created {
		i.grants.publishIssued(ctx, g, orderID)
	}
}

func (i *Issuer) issueAllLines(ctx context.Context, grants *Service, o *order.Order, tx *sql.Tx) ([]*domain.DownloadGrant, error) {
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

	// Take every downloadable product lock (including bundle leaves) in one
	// sorted pass before any grant insert. Locking in line order can deadlock
	// with a concurrent order that holds the same products in reverse.
	// Re-collect under the locks: a new required leaf means restart; a leaf
	// that became optional is simply skipped during issueForBundleLeaves.
	if tx != nil {
		ids, err := i.collectLockProductIDs(ctx, o, blocked)
		if err != nil {
			return nil, err
		}
		if err := i.lockProductsForIssue(ctx, tx, ids); err != nil {
			return nil, err
		}
		needed, err := i.collectLockProductIDs(ctx, o, blocked)
		if err != nil {
			return nil, err
		}
		if !lockSetCovers(ids, needed) {
			return nil, errIssueLockSetChanged
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
	// Product row locks are taken once in issueAllLines (sorted) before any
	// grant insert so concurrent orders cannot deadlock on opposite lock order.
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

// collectLockProductIDs gathers downloadable product IDs (including required
// bundle leaves) for non-blocked lines. Missing variants/products fail closed
// the same way issueForLine does so locking and issuance see the same set.
func (i *Issuer) collectLockProductIDs(ctx context.Context, o *order.Order, blocked map[string]struct{}) ([]string, error) {
	seen := make(map[string]struct{})
	var ids []string
	add := func(productID string) {
		productID = strings.TrimSpace(productID)
		if productID == "" {
			return
		}
		if _, ok := seen[productID]; ok {
			return
		}
		seen[productID] = struct{}{}
		ids = append(ids, productID)
	}
	for _, item := range o.Items() {
		orderItemID := domain.OrderItemID(o.ID, item.VariantID)
		if _, skip := blocked[orderItemID]; skip {
			continue
		}
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
		switch product.Type {
		case catalog.TypeDownloadable:
			add(product.ID)
		case catalog.TypeBundle:
			if i.components == nil {
				return nil, apperror.Internal("download: bundle components repository is required for bundle grant issuance")
			}
			rows, err := i.components.ListByBundleID(ctx, product.ID)
			if err != nil {
				return nil, fmt.Errorf("download: list bundle %s components: %w", product.ID, err)
			}
			for _, component := range rows {
				if !component.Required {
					continue
				}
				leaf, err := i.products.FindByID(ctx, component.ComponentProductID)
				if err != nil {
					return nil, fmt.Errorf("download: find bundle leaf %s: %w", component.ComponentProductID, err)
				}
				if leaf == nil || leaf.Type != catalog.TypeDownloadable {
					continue
				}
				add(leaf.ID)
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// lockSetCovers reports whether every needed product ID was already locked.
// Extra locked IDs (leaf became optional) are fine; missing IDs are not.
func lockSetCovers(locked, needed []string) bool {
	set := make(map[string]struct{}, len(locked))
	for _, id := range locked {
		set[id] = struct{}{}
	}
	for _, id := range needed {
		if _, ok := set[id]; !ok {
			return false
		}
	}
	return true
}

// lockProductsForIssue takes products FOR UPDATE in the given (sorted) order
// on the grant TX — the same row lock Replace/Attach use — so attachment list
// + IssueIdempotent cannot interleave with a join swap on those products.
func (i *Issuer) lockProductsForIssue(ctx context.Context, tx *sql.Tx, productIDs []string) error {
	for _, productID := range productIDs {
		if err := i.lockProductForIssue(ctx, tx, productID); err != nil {
			return err
		}
	}
	return nil
}

func (i *Issuer) lockProductForIssue(ctx context.Context, tx *sql.Tx, productID string) error {
	binder, ok := i.products.(productTxBinder)
	if !ok {
		return apperror.Internal("download: product repository does not support transactions")
	}
	bound := binder.WithTx(tx)
	locker, ok := bound.(lockingProductRepo)
	if !ok {
		return apperror.Internal("download: product repository does not support row locks")
	}
	if _, err := locker.LockForUpdate(ctx, productID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return apperror.Validation(fmt.Sprintf("download: product %s missing during grant issuance", productID))
		}
		return fmt.Errorf("download: lock product %s: %w", productID, err)
	}
	return nil
}
