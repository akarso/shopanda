package storecredit_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewIssueEntry_Success(t *testing.T) {
	amount, err := shared.NewMoney(500, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	e, err := storecredit.NewIssueEntry(id.New(), id.New(), amount, "return goodwill")
	if err != nil {
		t.Fatalf("NewIssueEntry: %v", err)
	}
	if e.Kind != storecredit.KindIssue {
		t.Fatalf("Kind = %q", e.Kind)
	}
}

func TestNewRedeemEntry_RequiresOrderID(t *testing.T) {
	amount, err := shared.NewMoney(100, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	_, err = storecredit.NewRedeemEntry(id.New(), id.New(), "", amount)
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}
