package returns

import (
	"context"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Service orchestrates the return (RMA) workflow.
type Service struct {
	returns  domainReturns.Repository
	orders   order.OrderRepository
	stock    inventory.StockRepository
	payments payment.PaymentRepository
	refunder payment.Refunder
	bus      *event.Bus
	log      logger.Logger
}

// NewService creates a return workflow service.
// refunder may be nil; provider refunds are skipped when unavailable.
func NewService(
	returns domainReturns.Repository,
	orders order.OrderRepository,
	stock inventory.StockRepository,
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
func (s *Service) Receive(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, item := range ret.Items() {
		entry, err := s.stock.GetStock(ctx, item.VariantID)
		if err != nil {
			return nil, fmt.Errorf("returns: get stock: %w", err)
		}
		entry.Quantity += item.Quantity
		entry.UpdatedAt = now
		if err := s.stock.SetStock(ctx, &entry); err != nil {
			return nil, fmt.Errorf("returns: restock: %w", err)
		}
	}
	if err := ret.MarkReceived(now); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.returns.Update(ctx, ret); err != nil {
		return nil, fmt.Errorf("returns: receive update: %w", err)
	}
	s.publish(ctx, domainReturns.EventReturnReceived, *ret)
	return ret, nil
}

// Refund transitions received → refunded and issues a provider refund when configured.
func (s *Service) Refund(ctx context.Context, returnID string) (*domainReturns.Return, error) {
	ret, err := s.loadReturn(ctx, returnID)
	if err != nil {
		return nil, err
	}
	total, err := ret.TotalAmount()
	if err != nil {
		return nil, fmt.Errorf("returns: total amount: %w", err)
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

	if s.refunder != nil && p.ProviderRef != "" {
		if _, err := s.refunder.Refund(ctx, p.ProviderRef, total.Amount(), total.Currency()); err != nil {
			return nil, fmt.Errorf("returns: provider refund: %w", err)
		}
	}

	now := time.Now().UTC()
	if err := ret.MarkRefunded(now); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.returns.Update(ctx, ret); err != nil {
		return nil, fmt.Errorf("returns: refund update: %w", err)
	}
	s.publish(ctx, domainReturns.EventReturnRefunded, *ret)
	return ret, nil
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
		if ret.Status() == domainReturns.StatusRejected || ret.Status() == domainReturns.StatusCancelled {
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
