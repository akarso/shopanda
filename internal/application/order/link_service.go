package order

import (
	"context"
	"database/sql"
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

// voucherCustomerLinker stamps vouchers.customer_id when a guest order is claimed.
type voucherCustomerLinker interface {
	LinkCustomer(ctx context.Context, orderID, customerID string) error
}

type voucherTxLinker interface {
	voucherCustomerLinker
	LinkerWithTx(tx *sql.Tx) voucherCustomerLinker
}

type orderTxBinder interface {
	WithTx(tx *sql.Tx) order.OrderRepository
}

type linkTxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// LinkOrderService handles account linking for claimed orders.
// Orchestrates registration + order linking workflow.
type LinkOrderService struct {
	orders    order.OrderRepository
	auth      OrderAuther
	jwt       *jwt.Issuer
	vouchers  voucherCustomerLinker
	txStarter linkTxStarter
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

// WithVouchers stamps vouchers.customer_id when guest orders are claimed.
func (s *LinkOrderService) WithVouchers(v voucherCustomerLinker) *LinkOrderService {
	if s == nil {
		return nil
	}
	s.vouchers = v
	return s
}

// WithTxStarter enables one transaction for order link + voucher customer stamp.
func (s *LinkOrderService) WithTxStarter(starter linkTxStarter) *LinkOrderService {
	if s == nil {
		return nil
	}
	s.txStarter = starter
	return s
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

	o, err := s.orders.FindByID(ctx, in.OrderID)
	if err != nil {
		return s.cleanupAndFail(ctx, createdCustomerID, fmt.Errorf("link order service: find order: %w", err))
	}
	if o == nil {
		return s.cleanupAndFail(ctx, createdCustomerID, fmt.Errorf("link order service: order not found"))
	}

	if err := o.LinkToCustomer(regOut.CustomerID); err != nil {
		return s.cleanupAndFail(ctx, createdCustomerID, fmt.Errorf("link order service: link order: %w", err))
	}

	if err := s.persistOrderLink(ctx, func(orders order.OrderRepository, vouchers voucherCustomerLinker) error {
		if err := orders.LinkToCustomer(ctx, o); err != nil {
			return fmt.Errorf("link order service: persist link: %w", err)
		}
		if vouchers != nil {
			if err := vouchers.LinkCustomer(ctx, o.ID, regOut.CustomerID); err != nil {
				return fmt.Errorf("link order service: link vouchers: %w", err)
			}
		}
		return nil
	}); err != nil {
		return s.cleanupAndFail(ctx, createdCustomerID, err)
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

	for i := range guestOrders {
		if err := guestOrders[i].LinkToCustomer(regOut.CustomerID); err != nil {
			return s.cleanupAndFail(ctx, regOut.CustomerID, fmt.Errorf("link order service: link order %s: %w", guestOrders[i].ID, err))
		}
	}

	if err := s.persistOrderLink(ctx, func(orders order.OrderRepository, vouchers voucherCustomerLinker) error {
		linked, err := orders.LinkToCustomerByContactEmail(ctx, in.ContactEmail, regOut.CustomerID, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("link order service: persist links: %w", err)
		}
		if linked == 0 {
			return fmt.Errorf("link order service: no claimable orders for this email")
		}
		if vouchers != nil {
			for i := range guestOrders {
				if err := vouchers.LinkCustomer(ctx, guestOrders[i].ID, regOut.CustomerID); err != nil {
					return fmt.Errorf("link order service: link vouchers: %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		return s.cleanupAndFail(ctx, regOut.CustomerID, err)
	}

	return RegisterAndLinkOutput{
		CustomerID: regOut.CustomerID,
		Email:      in.ContactEmail,
		Token:      regOut.Token,
		ExpiresAt:  regOut.ExpiresAt,
	}, nil
}

// persistOrderLink runs order + voucher customer stamps in one TX when both
// binders and a starter are available. Failure rolls the order link back so
// DeleteCustomer cleanup does not leave an orphan linked order.
func (s *LinkOrderService) persistOrderLink(ctx context.Context, fn func(orders order.OrderRepository, vouchers voucherCustomerLinker) error) error {
	orderTX, okOrder := s.orders.(orderTxBinder)
	voucherTX, okVoucher := s.vouchers.(voucherTxLinker)
	if s.txStarter != nil && okOrder && (s.vouchers == nil || okVoucher) {
		tx, err := s.txStarter.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("link order service: begin tx: %w", err)
		}
		defer tx.Rollback()
		var vouchers voucherCustomerLinker
		if s.vouchers != nil {
			vouchers = voucherTX.LinkerWithTx(tx)
		}
		if err := fn(orderTX.WithTx(tx), vouchers); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("link order service: commit: %w", err)
		}
		return nil
	}
	return fn(s.orders, s.vouchers)
}

// cleanupAndFail removes the just-registered customer after a linking failure
// so a retry of the claim flow is possible.
func (s *LinkOrderService) cleanupAndFail(ctx context.Context, customerID string, cause error) (RegisterAndLinkOutput, error) {
	if cleanupErr := s.auth.DeleteCustomer(ctx, customerID); cleanupErr != nil {
		return RegisterAndLinkOutput{}, fmt.Errorf("%w (cleanup customer: %v)", cause, cleanupErr)
	}
	return RegisterAndLinkOutput{}, cause
}
