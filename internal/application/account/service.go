package account

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// TxStarter begins database transactions.
type TxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// txConsentRepo is an optional capability: a ConsentRepository that supports
// transaction binding. Satisfied by *postgres.ConsentRepo.
type txConsentRepo interface {
	legal.ConsentRepository
	WithTx(tx *sql.Tx) legal.ConsentRepository
}

// txCustomerRepo is an optional capability: a CustomerRepository that supports
// transaction binding. Satisfied by *postgres.CustomerRepo.
type txCustomerRepo interface {
	customer.CustomerRepository
	WithTx(tx *sql.Tx) customer.CustomerRepository
}

// Service orchestrates account use cases (GDPR delete).
type Service struct {
	customers customer.CustomerRepository
	consents  legal.ConsentRepository
	bus       *event.Bus
	log       logger.Logger
	txStarter TxStarter
}

// NewService creates an account application service.
// txStarter may be nil; if nil, deletes are not wrapped in a transaction.
func NewService(
	customers customer.CustomerRepository,
	consents legal.ConsentRepository,
	bus *event.Bus,
	log logger.Logger,
	txStarter TxStarter,
) *Service {
	if customers == nil {
		panic("account: customers must not be nil")
	}
	if consents == nil {
		panic("account: consents must not be nil")
	}
	if bus == nil {
		panic("account: bus must not be nil")
	}
	if log == nil {
		panic("account: log must not be nil")
	}
	return &Service{
		customers: customers,
		consents:  consents,
		bus:       bus,
		log:       log,
		txStarter: txStarter,
	}
}

// DeleteAccount deletes consent data and the customer record inside a single
// transaction, then emits a customer.deleted event. The consents table has
// ON DELETE CASCADE so the explicit consent delete is a safety-net for non-DB
// backends.
func (s *Service) DeleteAccount(ctx context.Context, customerID string) error {
	if err := s.deleteInTx(ctx, customerID); err != nil {
		return err
	}

	evt := event.New(customer.EventCustomerDeleted, "account.service", customer.CustomerDeletedData{
		CustomerID: customerID,
	})
	if err := s.bus.Publish(ctx, evt); err != nil {
		s.log.Warn("account.event.publish_failed", map[string]interface{}{
			"event": customer.EventCustomerDeleted,
			"error": err.Error(),
		})
	}

	s.log.Info("account.deleted", map[string]interface{}{
		"customer_id": customerID,
	})

	return nil
}

// deleteInTx runs the consent + customer delete inside a transaction when a
// TxStarter is available. Falls back to sequential calls otherwise.
func (s *Service) deleteInTx(ctx context.Context, customerID string) error {
	if s.txStarter == nil {
		return s.deleteSequential(ctx, customerID)
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("account service: begin tx: %w", err)
	}

	txConsents, ok1 := s.consents.(txConsentRepo)
	txCustomers, ok2 := s.customers.(txCustomerRepo)
	if !ok1 || !ok2 {
		tx.Rollback()
		return s.deleteSequential(ctx, customerID)
	}

	consentRepo := txConsents.WithTx(tx)
	customerRepo := txCustomers.WithTx(tx)

	if err := consentRepo.DeleteByCustomerID(ctx, customerID); err != nil {
		tx.Rollback()
		return fmt.Errorf("account service: delete consent: %w", err)
	}

	if err := customerRepo.Delete(ctx, customerID); err != nil {
		tx.Rollback()
		return fmt.Errorf("account service: delete customer: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("account service: commit: %w", err)
	}
	return nil
}

func (s *Service) deleteSequential(ctx context.Context, customerID string) error {
	if err := s.consents.DeleteByCustomerID(ctx, customerID); err != nil {
		return fmt.Errorf("account service: delete consent: %w", err)
	}
	if err := s.customers.Delete(ctx, customerID); err != nil {
		return fmt.Errorf("account service: delete customer: %w", err)
	}
	return nil
}
