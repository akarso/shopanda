package customergroup_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewGroupPrice_Success(t *testing.T) {
	amount, err := shared.NewMoney(1500, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	p, err := customergroup.NewGroupPrice(id.New(), id.New(), id.New(), "", amount)
	if err != nil {
		t.Fatalf("NewGroupPrice: %v", err)
	}
	if p.Amount.Amount() != 1500 {
		t.Fatalf("Amount = %d", p.Amount.Amount())
	}
}

func TestNewGroupPrice_ZeroAmount(t *testing.T) {
	amount, err := shared.NewMoney(0, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	p, err := customergroup.NewGroupPrice(id.New(), id.New(), id.New(), "", amount)
	if err != nil {
		t.Fatalf("zero amount must be allowed: %v", err)
	}
	if !p.Amount.IsZero() {
		t.Fatalf("Amount = %d, want 0", p.Amount.Amount())
	}
}

func TestNewGroupPrice_NegativeAmount(t *testing.T) {
	amount, err := shared.NewMoney(-1, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	_, err = customergroup.NewGroupPrice(id.New(), id.New(), id.New(), "", amount)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}
