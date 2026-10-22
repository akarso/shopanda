package returns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
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

// Service orchestrates the return (RMA) workflow.
type Service struct {
	returns   domainReturns.Repository
	orders    order.OrderRepository
	stock     inventory.StockRepository
	variants  catalog.VariantRepository
	payments  payment.PaymentRepository
	refunder  payment.Refunder
	bus       *event.Bus
	log       logger.Logger
	txStarter TxStarter
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

	ret, err := domainReturns.NewReturn(id.New(), orderID, ord.CustomerID, reason, ord.Currency, returnItems)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.returns.Save(ctx, &ret); err != nil {
		return nil, fmt.Errorf("returns: save: %w", err)
	}
	s.publish(ctx, domainReturns.EventReturnRequested, ret)
	return &ret, nil
}

// Approve transitions requested → approved.
func (s *Service) Approve(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	if err := ret.Approve(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.returns.Update(ctx, ret); err != nil {
		return nil, fmt.Errorf("returns: approve update: %w", err)
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
	if err := s.returns.Update(ctx, ret); err != nil {
		return nil, fmt.Errorf("returns: reject update: %w", err)
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
	if err := s.returns.Update(ctx, ret); err != nil {
		return nil, fmt.Errorf("returns: cancel update: %w", err)
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
		if err := boundReturns.Update(ctx, locked); err != nil {
			return nil, fmt.Errorf("returns: receive update: %w", err)
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

	now := time.Now().UTC()
	if err := ret.MarkRefunded(now); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.returns.Update(ctx, ret); err != nil {
		return nil, fmt.Errorf("returns: refund update: %w", err)
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

	out := make([]ReturnableLine, 0, len(ord.Items()))
	for _, item := range ord.Items() {
		returnable := item.Quantity - already[item.VariantID]
		if returnable <= 0 {
			continue
		}
		out = append(out, ReturnableLine{
			VariantID:  item.VariantID,
			SKU:        item.SKU,
			Name:       item.Name,
			Ordered:    item.Quantity,
			Returnable: returnable,
			UnitPrice:  item.UnitPrice,
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
