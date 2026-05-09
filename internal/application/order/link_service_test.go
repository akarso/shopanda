package order_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/application/order"
	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/jwt"
)

// mockOrderAuther implements order.OrderAuther for testing.
type mockOrderAuther struct {
	registerOutput auth.RegisterOutput
	registerError  error
	deleteError    error
	deletedIDs     []string
}

func (m *mockOrderAuther) Register(ctx context.Context, in auth.RegisterInput) (auth.RegisterOutput, error) {
	if m.registerError != nil {
		return auth.RegisterOutput{}, m.registerError
	}
	return m.registerOutput, nil
}

func (m *mockOrderAuther) DeleteCustomer(ctx context.Context, customerID string) error {
	m.deletedIDs = append(m.deletedIDs, customerID)
	if m.deleteError != nil {
		return m.deleteError
	}
	return nil
}

func TestLinkOrderService_RegisterAndLink_Success(t *testing.T) {
	repo := newMockOrderRepository()
	mockAuth := &mockOrderAuther{
		registerOutput: auth.RegisterOutput{
			CustomerID: "cust-new",
			Token:      "jwt-token",
		},
	}
	jwtIssuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	svc := order.NewLinkOrderService(repo, mockAuth, jwtIssuer)

	// Create a guest order
	contactEmail := "guest@example.com"
	o := mustNewTestGuestOrder(t, contactEmail)
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Register and link
	in := order.RegisterAndLinkInput{
		OrderID:   o.ID,
		Email:     contactEmail,
		Password:  "SecurePass123",
		FirstName: "Jane",
		LastName:  "Doe",
	}

	out, err := svc.RegisterAndLink(context.Background(), in)
	if err != nil {
		t.Fatalf("RegisterAndLink: %v", err)
	}

	if out.CustomerID != "cust-new" {
		t.Errorf("CustomerID = %q, want %q", out.CustomerID, "cust-new")
	}
	if out.Email != contactEmail {
		t.Errorf("Email = %q, want %q", out.Email, contactEmail)
	}
	if out.Token == "" {
		t.Errorf("Token is empty, want non-empty JWT")
	}

	// Verify order was updated
	updated, err := repo.FindByID(context.Background(), o.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if updated == nil {
		t.Fatalf("order not found after link")
	}
	if updated.CustomerID != "cust-new" {
		t.Errorf("order.CustomerID = %q, want %q", updated.CustomerID, "cust-new")
	}
}

func TestLinkOrderService_RegisterAndLink_OrderNotFound(t *testing.T) {
	repo := newMockOrderRepository()
	mockAuth := &mockOrderAuther{
		registerOutput: auth.RegisterOutput{
			CustomerID: "cust-new",
			Token:      "jwt-token",
		},
	}
	jwtIssuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	svc := order.NewLinkOrderService(repo, mockAuth, jwtIssuer)

	in := order.RegisterAndLinkInput{
		OrderID:   "nonexistent",
		Email:     "guest@example.com",
		Password:  "SecurePass123",
		FirstName: "Jane",
		LastName:  "Doe",
	}

	_, err = svc.RegisterAndLink(context.Background(), in)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if len(mockAuth.deletedIDs) != 1 {
		t.Fatalf("DeleteCustomer call count = %d, want 1", len(mockAuth.deletedIDs))
	}
	if mockAuth.deletedIDs[0] != "cust-new" {
		t.Fatalf("DeleteCustomer called with %q, want %q", mockAuth.deletedIDs[0], "cust-new")
	}
}

func TestLinkOrderService_RegisterAndLink_AlreadyLinked(t *testing.T) {
	repo := newMockOrderRepository()
	mockAuth := &mockOrderAuther{
		registerOutput: auth.RegisterOutput{
			CustomerID: "cust-new",
			Token:      "jwt-token",
		},
	}
	jwtIssuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	svc := order.NewLinkOrderService(repo, mockAuth, jwtIssuer)

	// Create an authenticated order (already linked to customer)
	price := shared.MustNewMoney(1000, "EUR")
	item, err := domainOrder.NewItem("var-1", "SKU-001", "Test Product", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := domainOrder.NewOrder(id.New(), "cust-1", "", "EUR", []domainOrder.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	in := order.RegisterAndLinkInput{
		OrderID:   o.ID,
		Email:     "guest@example.com",
		Password:  "SecurePass123",
		FirstName: "Jane",
		LastName:  "Doe",
	}

	_, err = svc.RegisterAndLink(context.Background(), in)
	if err == nil {
		t.Fatalf("expected error for already-linked order, got nil")
	}
	if len(mockAuth.deletedIDs) != 1 {
		t.Fatalf("DeleteCustomer call count = %d, want 1", len(mockAuth.deletedIDs))
	}
	if mockAuth.deletedIDs[0] != "cust-new" {
		t.Fatalf("DeleteCustomer called with %q, want %q", mockAuth.deletedIDs[0], "cust-new")
	}
}

func TestLinkOrderService_RegisterAndLink_UpdateStatusFails_CleansUpCustomer(t *testing.T) {
	repo := newMockOrderRepository()
	repo.updateStatusErr = errors.New("db write failed")
	mockAuth := &mockOrderAuther{
		registerOutput: auth.RegisterOutput{
			CustomerID: "cust-new",
			Token:      "jwt-token",
		},
	}
	jwtIssuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	svc := order.NewLinkOrderService(repo, mockAuth, jwtIssuer)

	contactEmail := "guest@example.com"
	o := mustNewTestGuestOrder(t, contactEmail)
	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, err = svc.RegisterAndLink(context.Background(), order.RegisterAndLinkInput{
		OrderID:   o.ID,
		Email:     contactEmail,
		Password:  "SecurePass123",
		FirstName: "Jane",
		LastName:  "Doe",
	})
	if err == nil {
		t.Fatalf("expected update failure, got nil")
	}
	if len(mockAuth.deletedIDs) != 1 {
		t.Fatalf("DeleteCustomer call count = %d, want 1", len(mockAuth.deletedIDs))
	}
	if mockAuth.deletedIDs[0] != "cust-new" {
		t.Fatalf("DeleteCustomer called with %q, want %q", mockAuth.deletedIDs[0], "cust-new")
	}
}

func TestLinkOrderService_RegisterAndLink_EmptyInputs(t *testing.T) {
	repo := newMockOrderRepository()
	mockAuth := &mockOrderAuther{}
	jwtIssuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	svc := order.NewLinkOrderService(repo, mockAuth, jwtIssuer)

	tests := []struct {
		name  string
		input order.RegisterAndLinkInput
	}{
		{
			name: "empty order id",
			input: order.RegisterAndLinkInput{
				Email:    "guest@example.com",
				Password: "SecurePass123",
			},
		},
		{
			name: "empty email",
			input: order.RegisterAndLinkInput{
				OrderID:  "order-1",
				Password: "SecurePass123",
			},
		},
		{
			name: "empty password",
			input: order.RegisterAndLinkInput{
				OrderID: "order-1",
				Email:   "guest@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.RegisterAndLink(context.Background(), tt.input)
			if err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}
