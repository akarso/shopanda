package customer_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/customer"
)

func TestStatusIsValid(t *testing.T) {
	tests := []struct {
		status customer.Status
		want   bool
	}{
		{customer.StatusActive, true},
		{customer.StatusDisabled, true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.status.IsValid(); got != tt.want {
			t.Errorf("Status(%q).IsValid() = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestNewCustomer(t *testing.T) {
	c, err := customer.NewCustomer("cust-1", "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ID != "cust-1" {
		t.Errorf("ID = %q, want cust-1", c.ID)
	}
	if c.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", c.Email)
	}
	if c.Status != customer.StatusActive {
		t.Errorf("Status = %q, want active", c.Status)
	}
	if c.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if c.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestNewCustomer_EmptyID(t *testing.T) {
	_, err := customer.NewCustomer("", "alice@example.com")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewCustomer_EmptyEmail(t *testing.T) {
	_, err := customer.NewCustomer("cust-1", "")
	if err == nil {
		t.Fatal("expected error for empty email")
	}
}

func TestCustomer_Disable(t *testing.T) {
	c, _ := customer.NewCustomer("cust-1", "alice@example.com")
	if err := c.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if c.Status != customer.StatusDisabled {
		t.Errorf("Status = %q, want disabled", c.Status)
	}
}

func TestCustomer_Disable_AlreadyDisabled(t *testing.T) {
	c, _ := customer.NewCustomer("cust-1", "alice@example.com")
	_ = c.Disable()
	if err := c.Disable(); err == nil {
		t.Fatal("expected error when disabling already disabled customer")
	}
}

func TestCustomer_Enable(t *testing.T) {
	c, _ := customer.NewCustomer("cust-1", "alice@example.com")
	_ = c.Disable()
	if err := c.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if c.Status != customer.StatusActive {
		t.Errorf("Status = %q, want active", c.Status)
	}
}

func TestCustomer_Enable_AlreadyActive(t *testing.T) {
	c, _ := customer.NewCustomer("cust-1", "alice@example.com")
	if err := c.Enable(); err == nil {
		t.Fatal("expected error when enabling already active customer")
	}
}

func TestCustomer_SetPassword(t *testing.T) {
	c, _ := customer.NewCustomer("cust-1", "alice@example.com")
	if err := c.SetPassword("hashed-pw"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if c.PasswordHash != "hashed-pw" {
		t.Errorf("PasswordHash = %q, want hashed-pw", c.PasswordHash)
	}
}

func TestCustomer_SetPassword_Empty(t *testing.T) {
	c, _ := customer.NewCustomer("cust-1", "alice@example.com")
	if err := c.SetPassword(""); err == nil {
		t.Fatal("expected error for empty password hash")
	}
}
