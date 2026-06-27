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

func TestNewGroupPrice_InvalidAmount(t *testing.T) {
	amount, err := shared.NewMoney(0, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	_, err = customergroup.NewGroupPrice(id.New(), id.New(), id.New(), "", amount)
	if err == nil {
		t.Fatal("expected error for non-positive amount")
	}
}
