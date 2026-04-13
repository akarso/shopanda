package account

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Service orchestrates account use cases (GDPR delete).
type Service struct {
	customers customer.CustomerRepository
	consents  legal.ConsentRepository
	bus       *event.Bus
	log       logger.Logger
}

// NewService creates an account application service.
func NewService(
	customers customer.CustomerRepository,
	consents legal.ConsentRepository,
	bus *event.Bus,
	log logger.Logger,
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
	}
}

// DeleteAccount deletes consent data and the customer record, then emits
// a customer.deleted event. The consents table has ON DELETE CASCADE so the
// explicit consent delete is a safety-net for non-DB backends.
func (s *Service) DeleteAccount(ctx context.Context, customerID string) error {
	if err := s.consents.DeleteByCustomerID(ctx, customerID); err != nil {
		return fmt.Errorf("account service: delete consent: %w", err)
	}

	if err := s.customers.Delete(ctx, customerID); err != nil {
		return fmt.Errorf("account service: delete customer: %w", err)
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
