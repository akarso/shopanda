package storecredit

import (
	"context"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/shared"
	credit "github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Service orchestrates store credit use cases.
type Service struct {
	credits        credit.Repository
	customers      customer.CustomerRepository
	maxIssueAmount int64
}

// NewService creates a store credit service.
func NewService(credits credit.Repository, customers customer.CustomerRepository) *Service {
	if credits == nil {
		panic("storecredit: nil repository")
	}
	if customers == nil {
		panic("storecredit: nil customers repository")
	}
	return &Service{credits: credits, customers: customers}
}

// WithMaxIssueAmount caps a single Issue call at max minor units. A
// non-positive max disables the cap. Optional; if never called, Issue is
// unbounded. Returns the Service for chaining.
func (s *Service) WithMaxIssueAmount(max int64) *Service {
	s.maxIssueAmount = max
	return s
}

// Issue credits a customer account. idempotencyKey is optional; when set, a
// retried call with the same key is a no-op rather than crediting twice.
func (s *Service) Issue(ctx context.Context, customerID string, amount shared.Money, note, idempotencyKey string) error {
	if s.maxIssueAmount > 0 && amount.Amount() > s.maxIssueAmount {
		return apperror.Validation(fmt.Sprintf("amount exceeds maximum single issuance of %d", s.maxIssueAmount))
	}
	if err := s.ensureCustomer(ctx, customerID); err != nil {
		return err
	}
	if err := s.credits.Issue(ctx, customerID, amount, note, idempotencyKey); err != nil {
		return fmt.Errorf("storecredit: issue: %w", err)
	}
	return nil
}

// GetBalance returns the customer balance for a currency.
func (s *Service) GetBalance(ctx context.Context, customerID, currency string) (shared.Money, error) {
	balance, err := s.credits.GetBalance(ctx, customerID, currency)
	if err != nil {
		return shared.Money{}, fmt.Errorf("storecredit: get balance: %w", err)
	}
	return balance, nil
}

// Redeem debits store credit for an order.
func (s *Service) Redeem(ctx context.Context, customerID, orderID string, amount shared.Money) error {
	if err := s.credits.Redeem(ctx, customerID, orderID, amount); err != nil {
		if errors.Is(err, credit.ErrInsufficientBalance) {
			return apperror.Validation("insufficient store credit balance")
		}
		return fmt.Errorf("storecredit: redeem: %w", err)
	}
	return nil
}

// ListLedger returns paginated ledger entries.
func (s *Service) ListLedger(ctx context.Context, customerID, currency string, offset, limit int) ([]credit.Entry, error) {
	entries, err := s.credits.ListLedger(ctx, customerID, currency, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("storecredit: list ledger: %w", err)
	}
	return entries, nil
}

func (s *Service) ensureCustomer(ctx context.Context, customerID string) error {
	cust, err := s.customers.FindByID(ctx, customerID)
	if err != nil {
		return fmt.Errorf("storecredit: find customer: %w", err)
	}
	if cust == nil {
		return apperror.NotFound("customer not found")
	}
	return nil
}
