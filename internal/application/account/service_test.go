package account_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/application/account"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── mocks ───────────────────────────────────────────────────────────────

type mockCustomerRepo struct {
	deleteFn func(ctx context.Context, id string) error
}

func (m *mockCustomerRepo) FindByID(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (m *mockCustomerRepo) FindByEmail(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (m *mockCustomerRepo) Create(context.Context, *customer.Customer) error { return nil }
func (m *mockCustomerRepo) Update(context.Context, *customer.Customer) error { return nil }
func (m *mockCustomerRepo) ListCustomers(context.Context, int, int) ([]customer.Customer, error) {
	return nil, nil
}
func (m *mockCustomerRepo) BumpTokenGeneration(context.Context, string) error { return nil }
func (m *mockCustomerRepo) ChangePasswordAndBumpTokenGeneration(context.Context, string, string) error {
	return nil
}
func (m *mockCustomerRepo) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

type mockConsentRepo struct {
	deleteByCustomerIDFn func(ctx context.Context, customerID string) error
}

func (m *mockConsentRepo) FindByCustomerID(context.Context, string) (*legal.Consent, error) {
	return nil, nil
}
func (m *mockConsentRepo) Upsert(context.Context, *legal.Consent) error { return nil }
func (m *mockConsentRepo) DeleteByCustomerID(ctx context.Context, customerID string) error {
	if m.deleteByCustomerIDFn != nil {
		return m.deleteByCustomerIDFn(ctx, customerID)
	}
	return nil
}

func testLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

// ── tests ───────────────────────────────────────────────────────────────

func TestDeleteAccount_OK(t *testing.T) {
	var deletedCustomerID, deletedConsentID string

	customers := &mockCustomerRepo{
		deleteFn: func(_ context.Context, id string) error {
			deletedCustomerID = id
			return nil
		},
	}
	consents := &mockConsentRepo{
		deleteByCustomerIDFn: func(_ context.Context, id string) error {
			deletedConsentID = id
			return nil
		},
	}

	log := testLogger()
	bus := event.NewBus(log)
	var published bool
	bus.On(customer.EventCustomerDeleted, func(_ context.Context, e event.Event) error {
		data, ok := e.Data.(customer.CustomerDeletedData)
		if !ok {
			t.Fatalf("event data type = %T, want CustomerDeletedData", e.Data)
		}
		if data.CustomerID != "cust-42" {
			t.Fatalf("event CustomerID = %q, want cust-42", data.CustomerID)
		}
		published = true
		return nil
	})

	svc := account.NewService(customers, consents, bus, log, nil)
	err := svc.DeleteAccount(context.Background(), "cust-42")
	if err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if deletedConsentID != "cust-42" {
		t.Fatalf("consent deleted for %q, want cust-42", deletedConsentID)
	}
	if deletedCustomerID != "cust-42" {
		t.Fatalf("customer deleted for %q, want cust-42", deletedCustomerID)
	}
	if !published {
		t.Fatal("expected customer.deleted event to be published")
	}
}

func TestDeleteAccount_ConsentDeleteFails(t *testing.T) {
	customers := &mockCustomerRepo{}
	consents := &mockConsentRepo{
		deleteByCustomerIDFn: func(context.Context, string) error {
			return errors.New("consent gone")
		},
	}
	log := testLogger()
	bus := event.NewBus(log)
	svc := account.NewService(customers, consents, bus, log, nil)
	err := svc.DeleteAccount(context.Background(), "cust-1")
	if err == nil {
		t.Fatal("expected error when consent delete fails")
	}
}

func TestDeleteAccount_CustomerDeleteFails(t *testing.T) {
	customers := &mockCustomerRepo{
		deleteFn: func(context.Context, string) error {
			return errors.New("db down")
		},
	}
	consents := &mockConsentRepo{}
	log := testLogger()
	bus := event.NewBus(log)
	svc := account.NewService(customers, consents, bus, log, nil)
	err := svc.DeleteAccount(context.Background(), "cust-1")
	if err == nil {
		t.Fatal("expected error when customer delete fails")
	}
}

func TestNewService_Panics(t *testing.T) {
	log := testLogger()
	bus := event.NewBus(log)
	customers := &mockCustomerRepo{}
	consents := &mockConsentRepo{}

	cases := []struct {
		name string
		fn   func()
	}{
		{"nil customers", func() { account.NewService(nil, consents, bus, log, nil) }},
		{"nil consents", func() { account.NewService(customers, nil, bus, log, nil) }},
		{"nil bus", func() { account.NewService(customers, consents, nil, log, nil) }},
		{"nil log", func() { account.NewService(customers, consents, bus, nil, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.fn()
		})
	}
}
