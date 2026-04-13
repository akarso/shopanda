package legal_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/legal"
)

func TestNewConsent(t *testing.T) {
	c, err := legal.NewConsent("cust-1")
	if err != nil {
		t.Fatalf("NewConsent() error = %v", err)
	}
	if c.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", c.CustomerID)
	}
	if !c.Necessary {
		t.Error("Necessary should be true")
	}
	if c.Analytics {
		t.Error("Analytics should be false by default")
	}
	if c.Marketing {
		t.Error("Marketing should be false by default")
	}
}

func TestNewConsent_EmptyCustomerID(t *testing.T) {
	_, err := legal.NewConsent("")
	if err == nil {
		t.Error("expected error for empty customer_id")
	}
}

func TestConsent_Update(t *testing.T) {
	c, err := legal.NewConsent("cust-1")
	if err != nil {
		t.Fatalf("NewConsent() error = %v", err)
	}
	c.Update(true, true)

	if !c.Necessary {
		t.Error("Necessary should always be true")
	}
	if !c.Analytics {
		t.Error("Analytics should be true after update")
	}
	if !c.Marketing {
		t.Error("Marketing should be true after update")
	}
}

func TestConsent_Update_NecessaryAlwaysTrue(t *testing.T) {
	c, err := legal.NewConsent("cust-1")
	if err != nil {
		t.Fatalf("NewConsent() error = %v", err)
	}
	c.Update(false, false)

	if !c.Necessary {
		t.Error("Necessary must remain true regardless of update")
	}
}
