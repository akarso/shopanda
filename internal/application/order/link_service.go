package order

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	ExpiresAt  time.Time
}

// RegisterAndClaimInput contains registration fields for claiming all guest
// orders under a verified contact email.
type RegisterAndClaimInput struct {
	ContactEmail string
	Password     string
	FirstName    string
	LastName     string
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
	createdCustomerID := regOut.CustomerID

	// Fetch the order
	o, err := s.orders.FindByID(ctx, in.OrderID)
	if err != nil {
		if cleanupErr := s.auth.DeleteCustomer(ctx, createdCustomerID); cleanupErr != nil {
			return RegisterAndLinkOutput{}, fmt.Errorf("link order service: find order: %w (cleanup customer: %v)", err, cleanupErr)
		}
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: find order: %w", err)
	}
	if o == nil {
		if cleanupErr := s.auth.DeleteCustomer(ctx, createdCustomerID); cleanupErr != nil {
			return RegisterAndLinkOutput{}, fmt.Errorf("link order service: order not found (cleanup customer: %v)", cleanupErr)
		}
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: order not found")
	}

	// Link order to customer
	if err := o.LinkToCustomer(regOut.CustomerID); err != nil {
		if cleanupErr := s.auth.DeleteCustomer(ctx, createdCustomerID); cleanupErr != nil {
			return RegisterAndLinkOutput{}, fmt.Errorf("link order service: link order: %w (cleanup customer: %v)", err, cleanupErr)
		}
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: link order: %w", err)
	}

	// Persist the customer linking
	if err := s.orders.LinkToCustomer(ctx, o); err != nil {
		if cleanupErr := s.auth.DeleteCustomer(ctx, createdCustomerID); cleanupErr != nil {
			return RegisterAndLinkOutput{}, fmt.Errorf("link order service: persist link: %w (cleanup customer: %v)", err, cleanupErr)
		}
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: persist link: %w", err)
	}

	return RegisterAndLinkOutput{
		CustomerID: regOut.CustomerID,
		Email:      in.Email,
		Token:      regOut.Token,
		ExpiresAt:  regOut.ExpiresAt,
	}, nil
}

// RegisterAndClaimByEmail registers a new customer under a verified guest
// contact email and links every guest order carrying that contact email.
// The caller is responsible for having verified that the contact email
// belongs to the requester (claim token).
func (s *LinkOrderService) RegisterAndClaimByEmail(ctx context.Context, in RegisterAndClaimInput) (RegisterAndLinkOutput, error) {
	in.ContactEmail = strings.ToLower(strings.TrimSpace(in.ContactEmail))
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)

	if in.ContactEmail == "" {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: contact email must not be empty")
	}
	if in.Password == "" {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: password must not be empty")
	}

	guestOrders, err := s.orders.FindByContactEmail(ctx, in.ContactEmail)
	if err != nil {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: find guest orders: %w", err)
	}
	if len(guestOrders) == 0 {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: no claimable orders for this email")
	}

	regOut, err := s.auth.Register(ctx, auth.RegisterInput{
		Email:     in.ContactEmail,
		Password:  in.Password,
		FirstName: in.FirstName,
		LastName:  in.LastName,
	})
	if err != nil {
		return RegisterAndLinkOutput{}, fmt.Errorf("link order service: register: %w", err)
	}

	// Validate and mutate all domain objects before persisting anything so a
	// validation failure cannot leave a partially claimed set of orders.
	for i := range guestOrders {
		if err := guestOrders[i].LinkToCustomer(regOut.CustomerID); err != nil {
			return s.cleanupAndFail(ctx, regOut.CustomerID, fmt.Errorf("link order service: link order %s: %w", guestOrders[i].ID, err))
		}
	}
	for i := range guestOrders {
		if err := s.orders.LinkToCustomer(ctx, &guestOrders[i]); err != nil {
			return s.cleanupAndFail(ctx, regOut.CustomerID, fmt.Errorf("link order service: persist link %s: %w", guestOrders[i].ID, err))
		}
	}

	return RegisterAndLinkOutput{
		CustomerID: regOut.CustomerID,
		Email:      in.ContactEmail,
		Token:      regOut.Token,
		ExpiresAt:  regOut.ExpiresAt,
	}, nil
}

// cleanupAndFail removes the just-registered customer after a linking failure
// so a retry of the claim flow is possible.
func (s *LinkOrderService) cleanupAndFail(ctx context.Context, customerID string, cause error) (RegisterAndLinkOutput, error) {
	if cleanupErr := s.auth.DeleteCustomer(ctx, customerID); cleanupErr != nil {
		return RegisterAndLinkOutput{}, fmt.Errorf("%w (cleanup customer: %v)", cause, cleanupErr)
	}
	return RegisterAndLinkOutput{}, cause
}
