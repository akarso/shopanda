package order

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/jwt"
)

// OrderAuther abstracts auth service operations needed for order linking.
type OrderAuther interface {
	Register(ctx context.Context, in auth.RegisterInput) (auth.RegisterOutput, error)
	DeleteCustomer(ctx context.Context, customerID string) error
}

// LinkOrderService handles account linking for claimed orders.
// Orchestrates registration + order linking workflow.
type LinkOrderService struct {
	orders order.OrderRepository
	auth   OrderAuther
	jwt    *jwt.Issuer
}

// NewLinkOrderService creates a LinkOrderService.
func NewLinkOrderService(
	orders order.OrderRepository,
	auth OrderAuther,
	jwt *jwt.Issuer,
) *LinkOrderService {
	if orders == nil {
		panic("link order service: order repository must not be nil")
	}
	if auth == nil {
		panic("link order service: auth service must not be nil")
	}
	if jwt == nil {
		panic("link order service: jwt issuer must not be nil")
	}
	return &LinkOrderService{
		orders: orders,
		auth:   auth,
		jwt:    jwt,
	}
}

// RegisterAndLinkInput contains account registration and linking fields.
type RegisterAndLinkInput struct {
	OrderID   string
	Email     string
	Password  string
	FirstName string
	LastName  string
}

// RegisterAndLinkOutput contains registration result and auth token.
type RegisterAndLinkOutput struct {
	CustomerID string
	Email      string
	Token      string
}

// RegisterAndLink registers a new customer and links their claimed order in one transaction.
// Returns customer ID + JWT auth token for immediate login.
func (s *LinkOrderService) RegisterAndLink(ctx context.Context, in RegisterAndLinkInput) (RegisterAndLinkOutput, error) {
	in.OrderID = strings.TrimSpace(in.OrderID)
	in.Email = strings.TrimSpace(in.Email)
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)

	if in.OrderID == "" {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: order id must not be empty")
	}
	if in.Email == "" {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: email must not be empty")
	}
	if in.Password == "" {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: password must not be empty")
	}

	// Register the customer
	regOut, err := s.auth.Register(ctx, auth.RegisterInput{
		Email:     in.Email,
		Password:  in.Password,
		FirstName: in.FirstName,
		LastName:  in.LastName,
	})
	if err != nil {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: register: %w", err)
	}

	// Fetch the order
	o, err := s.orders.FindByID(ctx, in.OrderID)
	if err != nil {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: find order: %w", err)
	}
	if o == nil {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: order not found")
	}

	// Link order to customer
	if err := o.LinkToCustomer(regOut.CustomerID); err != nil {
		if cleanupErr := s.auth.DeleteCustomer(ctx, regOut.CustomerID); cleanupErr != nil {
			return RegisterAndLinkOutput{}, fmt.Errorf("link order service: link order: %w (cleanup customer: %v)", err, cleanupErr)
		}
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: link order: %w", err)
	}

	// Persist the updated order
	if err := s.orders.UpdateStatus(ctx, o); err != nil {
		if cleanupErr := s.auth.DeleteCustomer(ctx, regOut.CustomerID); cleanupErr != nil {
			return RegisterAndLinkOutput{}, fmt.Errorf("link order service: update order: %w (cleanup customer: %v)", err, cleanupErr)
		}
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: update order: %w", err)
	}

	return RegisterAndLinkOutput{
		CustomerID: regOut.CustomerID,
		Email:      in.Email,
		Token:      regOut.Token,
	}, nil
}
