package returns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	downloadapp "github.com/akarso/shopanda/internal/application/download"
	domainAssignment "github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainDownload "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// TxStarter begins the transaction that commits MarkReceived, restock, and
// RestockedAt together. *sql.DB satisfies it.
type TxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type txReturnRepo interface {
	domainReturns.Repository
	WithTx(tx *sql.Tx) domainReturns.Repository
}

type txStockRepo interface {
	inventory.StockRepository
	WithTx(tx *sql.Tx) inventory.StockRepository
}

type lockingStockRepo interface {
	GetStockForUpdate(ctx context.Context, variantID string) (inventory.StockEntry, error)
}

// restockReturnRepo locks the return row and records restock only while
// restocked_at is null. Both methods run on the transaction-bound repo.
type restockReturnRepo interface {
	LockForRestock(ctx context.Context, id string) (*domainReturns.Return, error)
	UpdateIfUnrestocked(ctx context.Context, ret *domainReturns.Return) error
}

// assignmentHooks is the ProductAssignment surface returns needs
// (PR-1072). *assignmentapp.Flow satisfies it.
type assignmentHooks interface {
	AssertParentReturnable(ctx context.Context, parentUnitID string, skipChildUnitIDs ...string) error
	RevokeForUnit(ctx context.Context, childUnitID, reason string) (*domainAssignment.ProductAssignment, error)
	BindsTx() bool
	WithTx(tx *sql.Tx) *assignmentapp.Flow
}

// grantHooks is the DownloadGrant surface returns needs (PR-1075).
// *downloadapp.Service satisfies it.
type grantHooks interface {
	RevokeForOrderItem(ctx context.Context, orderItemID, reason string) ([]*domainDownload.DownloadGrant, error)
	ListByOrderItem(ctx context.Context, orderItemID string) ([]domainDownload.DownloadGrant, error)
	BindsTx() bool
	WithTx(tx *sql.Tx) *downloadapp.Service
}

// Service orchestrates the return (RMA) workflow.
type Service struct {
	returns     domainReturns.Repository
	orders      order.OrderRepository
	stock       inventory.StockRepository
	variants    catalog.VariantRepository
	payments    payment.PaymentRepository
	refunder    payment.Refunder
	bus         *event.Bus
	log         logger.Logger
	txStarter   TxStarter
	assignments assignmentHooks
	grants      grantHooks
}

// WithTxStarter wires the transaction used by Receive. Without it, Receive
// fails closed before any status or stock write.
func (s *Service) WithTxStarter(starter TxStarter) *Service {
	if s == nil {
		return nil
	}
	s.txStarter = starter
	return s
}

// WithAssignmentFlow hooks ProductAssignment into returns (PR-1072).
// Nil skips the parent-with-children block and revoke-on-approve/refund
// (tests without assignment). Production always wires the flow.
// A typed-nil *Flow must clear the interface; storing it would make
// s.assignments != nil while method calls still hit a nil receiver.
func (s *Service) WithAssignmentFlow(flow *assignmentapp.Flow) *Service {
	if s == nil {
		return nil
	}
	if flow == nil {
		s.assignments = nil
	} else {
		s.assignments = flow
	}
	return s
}

// WithDownloadGrants hooks DownloadGrant revoke into approve/refund
// (PR-1075), same timing as assignment revoke. Nil skips grant revoke
// (tests without grants). Production always wires the service.
func (s *Service) WithDownloadGrants(grants *downloadapp.Service) *Service {
	if s == nil {
		return nil
	}
	if grants == nil {
		s.grants = nil
	} else {
		s.grants = grants
	}
	return s
}

// AssertNotOnOpenReturn fails when unitID appears on a return that still
// blocks rebinding: requested / approved / received, or refunded.
// Rejected and cancelled are skipped so a closed RMA does not trap the
// unit. Wired into assignment.Flow so AssignOwned / AdminAssign /
// RecordPurchase / ListOwnedParents cannot treat a refunded ticket as a
// fresh parent or child.
func (s *Service) AssertNotOnOpenReturn(ctx context.Context, unitID string) error {
	blocked, err := s.UnitsOnBlockingReturn(ctx, []string{unitID})
	if err != nil {
		return err
	}
	if len(blocked) > 0 {
		return apperror.Validation(assignmentapp.UnitOnOpenReturnMessage)
	}
	return nil
}

// UnitsOnBlockingReturn reports which of unitIDs appear on an open or
// refunded return. Loads returns once per unique order ID so
// ListOwnedParents does not N+1 FindByOrderID across sibling lines.
func (s *Service) UnitsOnBlockingReturn(ctx context.Context, unitIDs []string) (map[string]struct{}, error) {
	blocked := make(map[string]struct{})
	if s == nil {
		return blocked, nil
	}
	type unitRef struct {
		unitID    string
		variantID string
	}
	byOrder := make(map[string][]unitRef)
	seen := make(map[string]struct{}, len(unitIDs))
	for _, unitID := range unitIDs {
		unitID = strings.TrimSpace(unitID)
		if unitID == "" {
			continue
		}
		orderID, variantID, ok := domainAssignment.SplitUnitID(unitID)
		if !ok {
			continue
		}
		if canon, ok := domainAssignment.CanonicalUnitID(unitID); ok {
			unitID = canon
			orderID, variantID, _ = domainAssignment.SplitUnitID(canon)
		}
		if _, dup := seen[unitID]; dup {
			continue
		}
		seen[unitID] = struct{}{}
		byOrder[orderID] = append(byOrder[orderID], unitRef{unitID: unitID, variantID: variantID})
	}
	for orderID, refs := range byOrder {
		list, err := s.returns.FindByOrderID(ctx, orderID)
		if err != nil {
			return nil, fmt.Errorf("returns: open-return check: %w", err)
		}
		blockingVariants := make(map[string]struct{})
		for _, ret := range list {
			st := ret.Status()
			// Rejected/cancelled no longer block; refunded stays blocked so a
			// returned unit cannot be re-bound as if the RMA never happened.
			if st.IsTerminal() && st != domainReturns.StatusRefunded {
				continue
			}
			for _, item := range ret.Items() {
				blockingVariants[item.VariantID] = struct{}{}
			}
		}
		for _, ref := range refs {
			if _, ok := blockingVariants[ref.variantID]; ok {
				blocked[ref.unitID] = struct{}{}
			}
		}
	}
	return blocked, nil
}

// NewService creates a return workflow service.
// refunder may be nil; provider refunds are skipped when unavailable.
func NewService(
	returns domainReturns.Repository,
	orders order.OrderRepository,
	stock inventory.StockRepository,
	variants catalog.VariantRepository,
	payments payment.PaymentRepository,
	refunder payment.Refunder,
	bus *event.Bus,
	log logger.Logger,
) *Service {
	if returns == nil {
		panic("returns.NewService: nil returns repository")
	}
	if orders == nil {
		panic("returns.NewService: nil orders repository")
	}
	if stock == nil {
		panic("returns.NewService: nil stock repository")
	}
	if variants == nil {
		panic("returns.NewService: nil variants repository")
	}
	if payments == nil {
		panic("returns.NewService: nil payments repository")
	}
	if bus == nil {
		panic("returns.NewService: nil event bus")
	}
	if log == nil {
		panic("returns.NewService: nil logger")
	}
	return &Service{
		returns:  returns,
		orders:   orders,
		stock:    stock,
		variants: variants,
		payments: payments,
		refunder: refunder,
		bus:      bus,
		log:      log,
	}
}

// RequestLine identifies a quantity to return for an order variant.
type RequestLine struct {
	VariantID string
	Quantity  int
}

// RequestReturn creates a return in requested status after validating the order and quantities.
func (s *Service) RequestReturn(ctx context.Context, orderID, customerID, reason string, lines []RequestLine) (*domainReturns.Return, error) {
	if orderID == "" {
		return nil, apperror.Validation("order id must not be empty")
	}
	if reason == "" {
		return nil, apperror.Validation("reason must not be empty")
	}
	if len(lines) == 0 {
		return nil, apperror.Validation("at least one return line is required")
	}

	ord, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("returns: find order: %w", err)
	}
	if ord == nil {
		return nil, apperror.NotFound("order not found")
	}
	if ord.Status() != order.OrderStatusPaid {
		return nil, apperror.Validation("returns are only allowed for paid orders")
	}
	if customerID != "" && ord.CustomerID != "" && ord.CustomerID != customerID {
		return nil, apperror.Validation("order does not belong to customer")
	}

	orderItems := indexOrderItems(ord.Items())
	returnItems, err := s.buildReturnItems(orderItems, lines)
	if err != nil {
		return nil, err
	}
	if err := s.validateReturnQuantities(ctx, orderID, orderItems, returnItems); err != nil {
		return nil, err
	}
	if err := s.assertNoPartialDownloadableReturn(ctx, orderID, orderItems, returnItems); err != nil {
		return nil, err
	}
	if err := s.assertParentsReturnable(ctx, orderID, returnItems); err != nil {
		return nil, err
	}

	ret, err := domainReturns.NewReturn(id.New(), orderID, ord.CustomerID, reason, ord.Currency, returnItems)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.persistNewReturnLocked(ctx, &ret); err != nil {
		return nil, err
	}
	s.publish(ctx, domainReturns.EventReturnRequested, ret)
	return &ret, nil
}

// Approve transitions requested → approved.
//
// PR-1072: a parent unit with live assigned children not also on this
// return is refused. The status transition runs in memory first; then
// revoke + Update commit in one transaction so a failed persist leaves
// assignments intact. Refund also revokes as an idempotent safety net.
func (s *Service) Approve(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	if err := s.assertParentsReturnable(ctx, ret.OrderID, ret.Items()); err != nil {
		return nil, err
	}
	if err := ret.Approve(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.persistReturnWithAssignmentRevoke(ctx, ret, assignmentapp.RevokeReasonReturnApproved, "approve"); err != nil {
		return nil, err
	}
	s.publish(ctx, domainReturns.EventReturnApproved, *ret)
	return ret, nil
}

// Reject transitions requested → rejected.
func (s *Service) Reject(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	if err := ret.Reject(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := persistReturnUpdate(s.returns.Update(ctx, ret), "reject"); err != nil {
		return nil, err
	}
	s.publish(ctx, domainReturns.EventReturnRejected, *ret)
	return ret, nil
}

// Cancel transitions requested → cancelled.
func (s *Service) Cancel(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	if err := ret.Cancel(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := persistReturnUpdate(s.returns.Update(ctx, ret), "cancel"); err != nil {
		return nil, err
	}
	s.publish(ctx, domainReturns.EventReturnCancelled, *ret)
	return ret, nil
}

// Receive transitions approved → received and restocks inventory.
//
// Classification uses the order line's Restockable snapshot and runs before
// the transaction so a missing order line fails with no MarkReceived and no
// SetStock. Inside the transaction the return row is locked FOR UPDATE. If
// RestockedAt is already set, the transaction rolls back and Receive returns
// success without adding stock. Otherwise MarkReceived (when still approved),
// every GetStockForUpdate + SetStock, and RecordRestocked commit together.
// The restock update matches only while restocked_at IS NULL. Zero rows means
// another writer won: this transaction rolls back and Receive returns the
// stored return as an idempotent success.
//
// A row already stored as received with RestockedAt nil (the previous
// non-transactional Receive could stop between those writes) still restocks
// inside the same transaction and then sets RestockedAt. Do not retry those
// legacy rows if stock was already incremented: this path cannot see which
// lines the old Receive wrote. New failures do not land in that state.
func (s *Service) Receive(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	if ret.RestockedAt != nil {
		return ret, nil
	}
	if ret.Status() != domainReturns.StatusApproved && ret.Status() != domainReturns.StatusReceived {
		return nil, apperror.Validation("return is not in a receivable state")
	}
	// Re-check parent gate: an assignment may have landed after approve.
	if err := s.assertParentsReturnable(ctx, ret.OrderID, ret.Items()); err != nil {
		return nil, err
	}

	ord, err := s.orders.FindByID(ctx, ret.OrderID)
	if err != nil {
		return nil, fmt.Errorf("returns: find order: %w", err)
	}
	if ord == nil {
		return nil, apperror.NotFound("order not found")
	}
	orderItems := indexOrderItems(ord.Items())
	targets := make([]domainReturns.Item, 0, len(ret.Items()))
	for _, item := range ret.Items() {
		line, ok := orderItems[item.VariantID]
		if !ok {
			return nil, apperror.Wrap(apperror.CodeValidation, "return line is not on the order", fmt.Errorf("variant %s", item.VariantID))
		}
		if line.Restockable {
			targets = append(targets, item)
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].VariantID < targets[j].VariantID
	})

	if s.txStarter == nil {
		return nil, apperror.Internal("returns: transaction starter is required")
	}
	txReturns, ok := s.returns.(txReturnRepo)
	if !ok {
		return nil, apperror.Internal("returns: return repository does not support transactions")
	}
	txStock, ok := s.stock.(txStockRepo)
	if !ok {
		return nil, apperror.Internal("returns: stock repository does not support transactions")
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("returns: begin tx: %w", err)
	}
	defer tx.Rollback()

	boundReturns := txReturns.WithTx(tx)
	returnLocker, ok := boundReturns.(restockReturnRepo)
	if !ok {
		return nil, apperror.Internal("returns: return repository does not support restock locks")
	}
	boundStock := txStock.WithTx(tx)
	stockLocker, ok := boundStock.(lockingStockRepo)
	if !ok {
		return nil, apperror.Internal("returns: stock repository does not support row locks")
	}

	locked, err := returnLocker.LockForRestock(ctx, ret.ID)
	if err != nil {
		return nil, fmt.Errorf("returns: lock return: %w", err)
	}
	if locked == nil {
		return nil, apperror.NotFound("return not found")
	}
	if locked.RestockedAt != nil {
		return locked, nil
	}
	if locked.Status() != domainReturns.StatusApproved && locked.Status() != domainReturns.StatusReceived {
		return nil, apperror.Validation("return is not in a receivable state")
	}

	now := time.Now().UTC()
	if locked.Status() == domainReturns.StatusApproved {
		if err := locked.MarkReceived(); err != nil {
			return nil, apperror.Validation(err.Error())
		}
		if err := persistReturnUpdate(boundReturns.Update(ctx, locked), "receive"); err != nil {
			return nil, err
		}
	}

	type restocked struct {
		variantID string
		onHand    int
		delta     int
	}
	events := make([]restocked, 0, len(targets))
	for _, item := range targets {
		entry, err := stockLocker.GetStockForUpdate(ctx, item.VariantID)
		if err != nil {
			return nil, fmt.Errorf("returns: get stock: %w", err)
		}
		next, err := addRestockQty(entry.Quantity, item.Quantity)
		if err != nil {
			return nil, fmt.Errorf("returns: restock: %w", err)
		}
		entry.Quantity = next
		entry.UpdatedAt = now
		if err := boundStock.SetStock(ctx, &entry); err != nil {
			return nil, fmt.Errorf("returns: restock: %w", err)
		}
		events = append(events, restocked{variantID: item.VariantID, onHand: entry.Quantity, delta: item.Quantity})
	}
	if err := locked.RecordRestocked(now); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := returnLocker.UpdateIfUnrestocked(ctx, locked); err != nil {
		if errors.Is(err, domainReturns.ErrAlreadyRestocked) {
			fresh, ferr := s.returns.FindByID(ctx, ret.ID)
			if ferr != nil {
				return nil, fmt.Errorf("returns: reload restocked: %w", ferr)
			}
			if fresh == nil {
				return nil, apperror.NotFound("return not found")
			}
			return fresh, nil
		}
		return nil, fmt.Errorf("returns: receive restock update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("returns: receive commit: %w", err)
	}
	for _, ev := range events {
		s.publishStockUpdated(ctx, ev.variantID, ev.onHand, ev.delta)
	}
	s.publish(ctx, domainReturns.EventReturnReceived, *locked)
	return locked, nil
}

// Refund transitions received → refunded and issues a provider refund when configured.
//
// PR-1072: parent gate re-checks; status transition runs first; revoke +
// Update share one transaction. Revoke is idempotent so a return approved
// before assignment wiring, or a child rebound outside Flow, still ends
// revoked.
func (s *Service) Refund(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	if ret.Status() == domainReturns.StatusRefunded {
		return ret, nil
	}

	p, err := s.payments.FindByOrderID(ctx, ret.OrderID)
	if err != nil {
		return nil, fmt.Errorf("returns: find payment: %w", err)
	}
	if p == nil {
		return nil, apperror.Validation("payment not found for order")
	}
	if p.Status() != payment.StatusCompleted {
		return nil, apperror.Validation("payment is not in a refundable state")
	}

	if err := s.assertParentsReturnable(ctx, ret.OrderID, ret.Items()); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := ret.MarkRefunded(now); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.persistReturnWithAssignmentRevoke(ctx, ret, assignmentapp.RevokeReasonReturnRefunded, "refund"); err != nil {
		return nil, err
	}

	if s.refunder != nil && p.ProviderRef != "" {
		total, err := ret.TotalAmount()
		if err != nil {
			return nil, fmt.Errorf("returns: total amount: %w", err)
		}
		if _, err := s.refunder.Refund(ctx, p.ProviderRef, total.Amount(), total.Currency()); err != nil {
			return nil, fmt.Errorf("returns: provider refund: %w", err)
		}
	}

	s.publish(ctx, domainReturns.EventReturnRefunded, *ret)
	return ret, nil
}

// Get loads a return by ID.
func (s *Service) Get(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	return s.loadReturn(ctx, returnID)
}

// List returns paginated returns, newest first.
func (s *Service) List(ctx context.Context, offset, limit int) ([]domainReturns.Return, error) {
	if offset < 0 {
		return nil, apperror.Validation("offset must not be negative")
	}
	if limit <= 0 || limit > 100 {
		return nil, apperror.Validation("limit must be between 1 and 100")
	}
	list, err := s.returns.List(ctx, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("returns: list: %w", err)
	}
	return list, nil
}

// ListByOrderID returns all returns for an order.
func (s *Service) ListByOrderID(ctx context.Context, orderID string) ([]domainReturns.Return, error) {
	if orderID == "" {
		return nil, apperror.Validation("order id must not be empty")
	}
	list, err := s.returns.FindByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("returns: list by order: %w", err)
	}
	return list, nil
}

// ListByOrderForCustomer returns returns for an order after verifying customer ownership.
func (s *Service) ListByOrderForCustomer(ctx context.Context, orderID, customerID string) ([]domainReturns.Return, error) {
	if err := s.assertOrderCustomer(ctx, orderID, customerID); err != nil {
		return nil, err
	}
	return s.ListByOrderID(ctx, orderID)
}

// ListByCustomerID returns all returns for a customer.
func (s *Service) ListByCustomerID(ctx context.Context, customerID string) ([]domainReturns.Return, error) {
	if customerID == "" {
		return nil, apperror.Validation("customer id must not be empty")
	}
	list, err := s.returns.FindByCustomerID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("returns: list by customer: %w", err)
	}
	return list, nil
}

// ReturnableLine describes how many units of an order line can still be returned.
type ReturnableLine struct {
	VariantID  string
	SKU        string
	Name       string
	Ordered    int
	Returnable int
	UnitPrice  shared.Money
}

// ReturnableLines computes remaining returnable quantities for a paid order.
func (s *Service) ReturnableLines(ctx context.Context, orderID, customerID string) ([]ReturnableLine, error) {
	if err := s.assertOrderCustomer(ctx, orderID, customerID); err != nil {
		return nil, err
	}
	ord, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("returns: find order: %w", err)
	}
	if ord == nil {
		return nil, apperror.NotFound("order not found")
	}
	if ord.Status() != order.OrderStatusPaid {
		return nil, apperror.Validation("returns are only allowed for paid orders")
	}

	existing, err := s.returns.FindByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("returns: list existing: %w", err)
	}
	already := make(map[string]int)
	for _, ret := range existing {
		if ret.Status().IsTerminal() && ret.Status() != domainReturns.StatusRefunded {
			continue
		}
		for _, item := range ret.Items() {
			already[item.VariantID] += item.Quantity
		}
	}

	// Candidate lines still returnable on this order. Same-RMA skip must
	// use only lines that remain mutually returnable: a blocked child is
	// dropped from the skip set so its parent cannot stay listed alone.
	type candidateLine struct {
		item       order.Item
		returnable int
		unit       string
	}
	candidates := make([]candidateLine, 0, len(ord.Items()))
	for _, item := range ord.Items() {
		returnable := item.Quantity - already[item.VariantID]
		if returnable <= 0 {
			continue
		}
		candidates = append(candidates, candidateLine{
			item:       item,
			returnable: returnable,
			unit:       domainAssignment.UnitID(orderID, item.VariantID),
		})
	}

	if s.assignments != nil && len(candidates) > 0 {
		remaining := make([]candidateLine, len(candidates))
		copy(remaining, candidates)
		for {
			skip := make([]string, 0, len(remaining))
			for _, c := range remaining {
				skip = append(skip, c.unit)
			}
			next := make([]candidateLine, 0, len(remaining))
			dropped := false
			for _, c := range remaining {
				if err := s.assignments.AssertParentReturnable(ctx, c.unit, skip...); err != nil {
					if apperror.Is(err, apperror.CodeValidation) {
						dropped = true
						continue
					}
					return nil, err
				}
				next = append(next, c)
			}
			remaining = next
			if !dropped {
				break
			}
		}
		candidates = remaining
	}

	out := make([]ReturnableLine, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, ReturnableLine{
			VariantID:  c.item.VariantID,
			SKU:        c.item.SKU,
			Name:       c.item.Name,
			Ordered:    c.item.Quantity,
			Returnable: c.returnable,
			UnitPrice:  c.item.UnitPrice,
		})
	}
	return out, nil
}

func (s *Service) assertOrderCustomer(ctx context.Context, orderID, customerID string) error {
	ord, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("returns: find order: %w", err)
	}
	if ord == nil {
		return apperror.NotFound("order not found")
	}
	if customerID != "" && ord.CustomerID != "" && ord.CustomerID != customerID {
		return apperror.Forbidden("order not found")
	}
	return nil
}

// assertParentsReturnable blocks returning a unit that still has live
// assigned children not also listed on this return. Children that are
// themselves lines on the current request are skipped so same-order
// "return card and its tickets" succeeds. Ticket-only returns are
// unaffected. Nil assignment flow skips the gate.
func (s *Service) assertParentsReturnable(ctx context.Context, orderID string, items []domainReturns.Item) error {
	if s.assignments == nil {
		return nil
	}
	returning := make([]string, 0, len(items))
	for _, item := range items {
		returning = append(returning, domainAssignment.UnitID(orderID, item.VariantID))
	}
	for _, item := range items {
		unit := domainAssignment.UnitID(orderID, item.VariantID)
		if err := s.assignments.AssertParentReturnable(ctx, unit, returning...); err != nil {
			return err
		}
	}
	return nil
}

// persistNewReturnLocked saves a new RMA under the same per-unit advisory
// locks assignment writes take, then re-checks the parent gate so an
// AssignOwned that raced the first check cannot land under an open RMA.
func (s *Service) persistNewReturnLocked(ctx context.Context, ret *domainReturns.Return) error {
	if ret == nil {
		return nil
	}
	txReturns, ok := s.returns.(txReturnRepo)
	// Only serialize with assignment writes when the flow is wired.
	if s.assignments == nil || s.txStarter == nil || !ok {
		return persistReturnSave(s.returns.Save(ctx, ret))
	}
	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("returns: begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := assignmentapp.LockAssignmentUnits(ctx, tx, returnUnitIDs(ret)...); err != nil {
		return err
	}
	if err := s.assertParentsReturnable(ctx, ret.OrderID, ret.Items()); err != nil {
		return err
	}
	if err := persistReturnSave(txReturns.WithTx(tx).Save(ctx, ret)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("returns: save commit: %w", err)
	}
	return nil
}

func persistReturnSave(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("returns: save: %w", err)
}

func returnUnitIDs(ret *domainReturns.Return) []string {
	if ret == nil {
		return nil
	}
	out := make([]string, 0, len(ret.Items()))
	for _, item := range ret.Items() {
		out = append(out, domainAssignment.UnitID(ret.OrderID, item.VariantID))
	}
	return out
}

// persistReturnWithAssignmentRevoke commits assignment + download-grant
// revoke + return Update in one transaction when either hook is wired.
// Domain transition must already have succeeded in memory. Without both
// hooks, updates the return directly.
func (s *Service) persistReturnWithAssignmentRevoke(ctx context.Context, ret *domainReturns.Return, revokeReason, op string) error {
	if s.assignments == nil && s.grants == nil {
		return persistReturnUpdate(s.returns.Update(ctx, ret), op)
	}
	if s.txStarter == nil {
		return apperror.Internal("returns: transaction starter is required")
	}
	if s.assignments != nil && !s.assignments.BindsTx() {
		return apperror.Internal("returns: assignment repository does not support transactions")
	}
	if s.grants != nil && !s.grants.BindsTx() {
		return apperror.Internal("returns: download grant repository does not support transactions")
	}
	txReturns, ok := s.returns.(txReturnRepo)
	if !ok {
		return apperror.Internal("returns: return repository does not support transactions")
	}
	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("returns: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Serialize against AssignOwned / AdminAssign / RecordPurchase on the
	// same unit ids (advisory locks + their open-return recheck).
	if s.assignments != nil {
		if err := assignmentapp.LockAssignmentUnits(ctx, tx, returnUnitIDs(ret)...); err != nil {
			return err
		}
		if err := s.assertParentsReturnable(ctx, ret.OrderID, ret.Items()); err != nil {
			return err
		}
	}

	var revoked []*domainAssignment.ProductAssignment
	if s.assignments != nil {
		bound := s.assignments.WithTx(tx)
		revoked, err = revokeAssignmentsForReturn(ctx, bound, ret, revokeReason)
		if err != nil {
			return err
		}
	}
	var revokedGrants []*domainDownload.DownloadGrant
	if s.grants != nil {
		boundGrants := s.grants.WithTx(tx)
		revokedGrants, err = revokeGrantsForReturn(ctx, boundGrants, ret, revokeReason)
		if err != nil {
			return err
		}
	}
	boundReturns := txReturns.WithTx(tx)
	if err := persistReturnUpdate(boundReturns.Update(ctx, ret), op); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("returns: %s commit: %w", op, err)
	}
	for _, a := range revoked {
		s.publishAssignmentRevoked(ctx, a, revokeReason)
	}
	for _, g := range revokedGrants {
		s.publishGrantRevoked(ctx, g, revokeReason)
	}
	return nil
}

// assertNoPartialDownloadableReturn rejects partial qty on lines that already
// have DownloadGrant rows. Until download_limit scaling exists, a return of a
// downloadable line must cover the full ordered quantity (approve/refund then
// fully revokes). Lines without grants are unaffected (virtual / non-digital).
func (s *Service) assertNoPartialDownloadableReturn(
	ctx context.Context,
	orderID string,
	orderItems map[string]order.Item,
	items []domainReturns.Item,
) error {
	if s == nil || s.grants == nil {
		return nil
	}
	for _, item := range items {
		ordered := orderItems[item.VariantID].Quantity
		if item.Quantity >= ordered {
			continue
		}
		orderItemID := domainDownload.OrderItemID(orderID, item.VariantID)
		rows, err := s.grants.ListByOrderItem(ctx, orderItemID)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			return apperror.Validation("partial return is not supported for downloadable items")
		}
	}
	return nil
}

// revokeGrantsForReturn revokes DownloadGrant rows for each return line.
func revokeGrantsForReturn(ctx context.Context, hooks grantHooks, ret *domainReturns.Return, reason string) ([]*domainDownload.DownloadGrant, error) {
	if hooks == nil || ret == nil {
		return nil, nil
	}
	out := make([]*domainDownload.DownloadGrant, 0)
	for _, item := range ret.Items() {
		orderItemID := domainDownload.OrderItemID(ret.OrderID, item.VariantID)
		revoked, err := hooks.RevokeForOrderItem(ctx, orderItemID, reason)
		if err != nil {
			return nil, err
		}
		out = append(out, revoked...)
	}
	return out, nil
}

func (s *Service) publishGrantRevoked(ctx context.Context, g *domainDownload.DownloadGrant, reason string) {
	if s == nil || g == nil || s.bus == nil {
		return
	}
	data := domainDownload.GrantRevokedData{
		GrantID:     g.ID,
		OrderItemID: g.OrderItemID,
		CustomerID:  g.CustomerID,
		AssetID:     g.AssetID,
		Reason:      reason,
	}
	if err := s.bus.Publish(ctx, event.New(domainDownload.EventDownloadGrantRevoked, "returns.service", data)); err != nil {
		s.log.Warn("returns: publish download grant revoked failed", map[string]interface{}{
			"event":    domainDownload.EventDownloadGrantRevoked,
			"grant_id": g.ID,
			"error":    err.Error(),
		})
	}
}

// revokeAssignmentsForReturn revokes the current ProductAssignment for
// each return line's child unit (assigned or never-assigned unassigned).
// Missing rows are skipped. Returns the rows that were revoked for event
// publishing after commit.
func revokeAssignmentsForReturn(ctx context.Context, hooks assignmentHooks, ret *domainReturns.Return, reason string) ([]*domainAssignment.ProductAssignment, error) {
	if hooks == nil || ret == nil {
		return nil, nil
	}
	out := make([]*domainAssignment.ProductAssignment, 0, len(ret.Items()))
	for _, item := range ret.Items() {
		unit := domainAssignment.UnitID(ret.OrderID, item.VariantID)
		revoked, err := hooks.RevokeForUnit(ctx, unit, reason)
		if err != nil {
			return nil, err
		}
		if revoked != nil {
			out = append(out, revoked)
		}
	}
	return out, nil
}

func (s *Service) publishAssignmentRevoked(ctx context.Context, a *domainAssignment.ProductAssignment, reason string) {
	if s == nil || a == nil {
		return
	}
	data := domainAssignment.AssignmentRevokedData{
		AssignmentID:      a.ID,
		ChildOrderItemID:  a.ChildOrderItemID,
		ParentOrderItemID: a.ParentOrderItemID,
		Reason:            reason,
	}
	if err := s.bus.Publish(ctx, event.New(domainAssignment.EventAssignmentRevoked, "returns.service", data)); err != nil {
		s.log.Warn("returns: publish assignment revoked failed", map[string]interface{}{
			"event":         domainAssignment.EventAssignmentRevoked,
			"assignment_id": a.ID,
			"reason":        reason,
			"error":         err.Error(),
		})
	}
}

// persistReturnUpdate maps a lost status check to a conflict. Other update
// failures stay wrapped with op so the caller can see which transition failed.
func persistReturnUpdate(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domainReturns.ErrStatusConflict) {
		return apperror.Conflict("return status changed")
	}
	return fmt.Errorf("returns: %s update: %w", op, err)
}

func (s *Service) loadReturn(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	if returnID == "" {
		return nil, apperror.Validation("return id must not be empty")
	}
	ret, err := s.returns.FindByID(ctx, returnID)
	if err != nil {
		return nil, fmt.Errorf("returns: find: %w", err)
	}
	if ret == nil {
		return nil, apperror.NotFound("return not found")
	}
	return ret, nil
}

func indexOrderItems(items []order.Item) map[string]order.Item {
	out := make(map[string]order.Item, len(items))
	for _, item := range items {
		out[item.VariantID] = item
	}
	return out
}

func (s *Service) buildReturnItems(orderItems map[string]order.Item, lines []RequestLine) ([]domainReturns.Item, error) {
	out := make([]domainReturns.Item, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		if line.VariantID == "" {
			return nil, apperror.Validation("variant id must not be empty")
		}
		if line.Quantity <= 0 {
			return nil, apperror.Validation("quantity must be positive")
		}
		if _, dup := seen[line.VariantID]; dup {
			return nil, apperror.Validation("duplicate variant in return request")
		}
		seen[line.VariantID] = struct{}{}

		orderItem, ok := orderItems[line.VariantID]
		if !ok {
			return nil, apperror.Validation("variant not found on order")
		}
		if line.Quantity > orderItem.Quantity {
			return nil, apperror.Validation("return quantity exceeds ordered quantity")
		}
		item, err := domainReturns.NewItem(orderItem.VariantID, orderItem.SKU, orderItem.Name, line.Quantity, orderItem.UnitPrice)
		if err != nil {
			return nil, apperror.Validation(err.Error())
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) validateReturnQuantities(ctx context.Context, orderID string, orderItems map[string]order.Item, newItems []domainReturns.Item) error {
	existing, err := s.returns.FindByOrderID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("returns: list existing: %w", err)
	}
	already := make(map[string]int)
	for _, ret := range existing {
		if ret.Status().IsTerminal() && ret.Status() != domainReturns.StatusRefunded {
			continue
		}
		for _, item := range ret.Items() {
			already[item.VariantID] += item.Quantity
		}
	}
	for _, item := range newItems {
		ordered := orderItems[item.VariantID].Quantity
		if already[item.VariantID]+item.Quantity > ordered {
			return apperror.Validation("return quantity exceeds remaining returnable quantity")
		}
	}
	return nil
}

func (s *Service) publish(ctx context.Context, name string, ret domainReturns.Return) {
	data := domainReturns.ReturnEventData{
		ReturnID:   ret.ID,
		OrderID:    ret.OrderID,
		Status:     ret.Status(),
		CustomerID: ret.CustomerID,
	}
	if err := s.bus.Publish(ctx, event.New(name, "returns.service", data)); err != nil {
		s.log.Warn("returns: publish event failed", map[string]interface{}{
			"event":     name,
			"return_id": ret.ID,
			"error":     err.Error(),
		})
	}
}

// addRestockQty adds a returned quantity onto on-hand stock. Both values are
// non-negative; a sum past MaxInt is an error so Receive does not wrap.
func addRestockQty(onHand, add int) (int, error) {
	if onHand < 0 || add < 1 {
		return 0, fmt.Errorf("quantity must be positive")
	}
	if onHand > math.MaxInt-add {
		return 0, fmt.Errorf("quantity overflow")
	}
	return onHand + add, nil
}

// publishStockUpdated publishes inventory.EventStockUpdated for one
// restocked item (PR-1049: returns restocking was another stock-changing
// path PR-1036's on-save search subscriber never heard about). Unlike
// publish above, a failed variant lookup or Publish error here is logged
// but never returned — Receive has already durably recorded the restock
// and the return's own state transition by this point; a stale search
// index entry is not worth turning a successful restock into a reported
// failure over.
func (s *Service) publishStockUpdated(ctx context.Context, variantID string, onHand, delta int) {
	variant, err := s.variants.FindByID(ctx, variantID)
	if err != nil {
		s.log.Warn("returns: variant lookup failed for stock event", map[string]interface{}{
			"variant_id": variantID,
			"error":      err.Error(),
		})
		return
	}
	if variant == nil {
		return
	}
	data := inventory.StockUpdatedData{
		ProductID: variant.ProductID,
		VariantID: variant.ID,
		SKU:       variant.SKU,
		OnHand:    inventory.Qty(onHand),
		Delta:     inventory.Qty(delta),
	}
	if err := s.bus.Publish(ctx, event.New(inventory.EventStockUpdated, "returns.service", data)); err != nil {
		s.log.Warn("returns: publish stock event failed", map[string]interface{}{
			"variant_id": variantID,
			"error":      err.Error(),
		})
	}
}
