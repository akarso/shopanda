package returns_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func mustItem(t *testing.T, variantID string, qty int, amount int64) returns.Item {
	t.Helper()
	item, err := returns.NewItem(variantID, "SKU-"+variantID, "Product "+variantID, qty, shared.MustNewMoney(amount, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	return item
}

func TestNewReturn_Valid(t *testing.T) {
	ret, err := returns.NewReturn("r1", "o1", "c1", "damaged", "EUR", []returns.Item{
		mustItem(t, "v1", 1, 1000),
	})
	if err != nil {
		t.Fatalf("NewReturn: %v", err)
	}
	if ret.Status() != returns.StatusRequested {
		t.Fatalf("status = %q", ret.Status())
	}
}

func TestReturn_ApproveReceiveRefund(t *testing.T) {
	ret, err := returns.NewReturn("r1", "o1", "c1", "wrong size", "EUR", []returns.Item{
		mustItem(t, "v1", 2, 1500),
	})
	if err != nil {
		t.Fatalf("NewReturn: %v", err)
	}

	if err := ret.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	now := time.Now().UTC()
	if err := ret.MarkReceived(); err != nil {
		t.Fatalf("MarkReceived: %v", err)
	}
	if ret.RestockedAt != nil {
		t.Fatal("restocked_at should not be set until RecordRestocked")
	}
	if err := ret.RecordRestocked(now); err != nil {
		t.Fatalf("RecordRestocked: %v", err)
	}
	if ret.RestockedAt == nil {
		t.Fatal("expected restocked_at")
	}
	if err := ret.MarkRefunded(now); err != nil {
		t.Fatalf("MarkRefunded: %v", err)
	}
	if ret.Status() != returns.StatusRefunded {
		t.Fatalf("status = %q", ret.Status())
	}
	total, err := ret.TotalAmount()
	if err != nil {
		t.Fatalf("TotalAmount: %v", err)
	}
	if total.Amount() != 3000 {
		t.Fatalf("total = %d, want 3000", total.Amount())
	}
}

func TestReturn_RejectFromRequested(t *testing.T) {
	ret, _ := returns.NewReturn("r1", "o1", "", "changed mind", "EUR", []returns.Item{
		mustItem(t, "v1", 1, 500),
	})
	if err := ret.Reject(); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if ret.Status() != returns.StatusRejected {
		t.Fatalf("status = %q", ret.Status())
	}
}

func TestReturn_CannotApproveTwice(t *testing.T) {
	ret, _ := returns.NewReturn("r1", "o1", "", "defect", "EUR", []returns.Item{
		mustItem(t, "v1", 1, 500),
	})
	_ = ret.Approve()
	if err := ret.Approve(); err == nil {
		t.Fatal("expected error on second approve")
	}
}

func TestReturn_TotalAmountOverflow(t *testing.T) {
	ret, _ := returns.NewReturn("r1", "o1", "", "bulk", "EUR", []returns.Item{
		mustItem(t, "v1", 1, 9223372036854770000),
		mustItem(t, "v2", 2, 9223372036854770000),
	})
	if _, err := ret.TotalAmount(); err == nil {
		t.Fatal("expected overflow error")
	}
}
