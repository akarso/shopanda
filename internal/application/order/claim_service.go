package order

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/order"
)

// ClaimService handles guest order discovery and claiming operations.
type ClaimService struct {
	orders order.OrderRepository
}

// NewClaimService creates a ClaimService.
func NewClaimService(orders order.OrderRepository) *ClaimService {
	if orders == nil {
		panic("order claim service: order repository must not be nil")
	}
	return &ClaimService{orders: orders}
}

// SearchGuestOrders finds orders by contact email for guest discovery.
// Returns empty slice if no orders found.
func (s *ClaimService) SearchGuestOrders(ctx context.Context, contactEmail string) ([]order.Order, error) {
	if strings.TrimSpace(contactEmail) == "" {
		return nil, fmt.Errorf("order claim service: contact email must not be empty")
	}
	orders, err := s.orders.FindByContactEmail(ctx, contactEmail)
	if err != nil {
		return nil, fmt.Errorf("order claim service: search orders: %w", err)
	}
	return orders, nil
}

// VerifyOrderBelongsToEmail validates that an order matches the contact email.
// Used to verify claim token integrity before linking.
func (s *ClaimService) VerifyOrderBelongsToEmail(ctx context.Context, orderID, contactEmail string) (*order.Order, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, fmt.Errorf("order claim service: order id must not be empty")
	}
	if strings.TrimSpace(contactEmail) == "" {
		return nil, fmt.Errorf("order claim service: contact email must not be empty")
	}

	o, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("order claim service: find order: %w", err)
	}
	if o == nil {
		return nil, fmt.Errorf("order claim service: order ownership verification failed")
	}

	contactEmailNorm := strings.ToLower(strings.TrimSpace(contactEmail))
	orderEmailNorm := strings.ToLower(strings.TrimSpace(o.ContactEmail))
	if orderEmailNorm != contactEmailNorm {
		return nil, fmt.Errorf("order claim service: order ownership verification failed")
	}

	return o, nil
}
