package order_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

func validItem(t *testing.T) order.Item {
	t.Helper()
	price := shared.MustNewMoney(1000, "EUR")
	item, err := order.NewItem("variant-1", "SKU-001", "Blue Shirt", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	return item
}

func TestNewOrder_Success(t *testing.T) {
	item := validItem(t)
	o, err := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if o.Status() != order.OrderStatusPending {
		t.Errorf("Status = %q, want pending", o.Status())
	}
	if o.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", o.CustomerID)
	}
	if o.TotalAmount.Amount() != 2000 {
		t.Errorf("TotalAmount = %d, want 2000", o.TotalAmount.Amount())
	}
	if len(o.Items) != 1 {
		t.Errorf("Items = %d, want 1", len(o.Items))
	}
}

func TestNewOrder_EmptyID(t *testing.T) {
	item := validItem(t)
	_, err := order.NewOrder("", "cust-1", "EUR", []order.Item{item})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewOrder_EmptyCustomerID(t *testing.T) {
	item := validItem(t)
	_, err := order.NewOrder(id.New(), "", "EUR", []order.Item{item})
	if err == nil {
		t.Fatal("expected error for empty customer id")
	}
}

func TestNewOrder_InvalidCurrency(t *testing.T) {
	item := validItem(t)
	_, err := order.NewOrder(id.New(), "cust-1", "xx", []order.Item{item})
	if err == nil {
		t.Fatal("expected error for invalid currency")
	}
}

func TestNewOrder_NoItems(t *testing.T) {
	_, err := order.NewOrder(id.New(), "cust-1", "EUR", nil)
	if err == nil {
		t.Fatal("expected error for no items")
	}
}

func TestNewOrder_CurrencyMismatch(t *testing.T) {
	price := shared.MustNewMoney(1000, "USD")
	item, _ := order.NewItem("v-1", "SKU", "Shirt", 1, price)
	_, err := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err == nil {
		t.Fatal("expected error for currency mismatch")
	}
}

func TestNewOrder_MultipleItems(t *testing.T) {
	p1 := shared.MustNewMoney(1000, "EUR")
	p2 := shared.MustNewMoney(500, "EUR")
	i1, _ := order.NewItem("v-1", "SKU-1", "Shirt", 2, p1)
	i2, _ := order.NewItem("v-2", "SKU-2", "Hat", 3, p2)
	o, err := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{i1, i2})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	// 1000*2 + 500*3 = 3500
	if o.TotalAmount.Amount() != 3500 {
		t.Errorf("TotalAmount = %d, want 3500", o.TotalAmount.Amount())
	}
}

func TestOrder_Confirm(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err := o.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if o.Status() != order.OrderStatusConfirmed {
		t.Errorf("Status = %q, want confirmed", o.Status())
	}
}

func TestOrder_Confirm_NotPending(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	_ = o.Confirm()
	if err := o.Confirm(); err == nil {
		t.Fatal("expected error confirming non-pending order")
	}
}

func TestOrder_MarkPaid(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	_ = o.Confirm()
	if err := o.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if o.Status() != order.OrderStatusPaid {
		t.Errorf("Status = %q, want paid", o.Status())
	}
}

func TestOrder_MarkPaid_NotConfirmed(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err := o.MarkPaid(); err == nil {
		t.Fatal("expected error marking unconfirmed order as paid")
	}
}

func TestOrder_Cancel_Pending(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err := o.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if o.Status() != order.OrderStatusCancelled {
		t.Errorf("Status = %q, want cancelled", o.Status())
	}
}

func TestOrder_Cancel_Confirmed(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	_ = o.Confirm()
	if err := o.Cancel(); err != nil {
		t.Fatalf("Cancel confirmed: %v", err)
	}
	if o.Status() != order.OrderStatusCancelled {
		t.Errorf("Status = %q, want cancelled", o.Status())
	}
}

func TestOrder_Cancel_Paid(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	_ = o.Confirm()
	_ = o.MarkPaid()
	if err := o.Cancel(); err == nil {
		t.Fatal("expected error cancelling paid order")
	}
}

func TestOrder_Fail(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err := o.Fail(); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if o.Status() != order.OrderStatusFailed {
		t.Errorf("Status = %q, want failed", o.Status())
	}
}

func TestOrder_Fail_NotPending(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	_ = o.Confirm()
	if err := o.Fail(); err == nil {
		t.Fatal("expected error failing non-pending order")
	}
}

func TestOrderStatus_IsValid(t *testing.T) {
	for _, s := range []order.OrderStatus{
		order.OrderStatusPending, order.OrderStatusConfirmed,
		order.OrderStatusPaid, order.OrderStatusCancelled,
		order.OrderStatusFailed,
	} {
		if !s.IsValid() {
			t.Errorf("IsValid(%q) = false, want true", s)
		}
	}
	if order.OrderStatus("bogus").IsValid() {
		t.Error("bogus status should be invalid")
	}
}

func TestSetStatusFromDB(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err := o.SetStatusFromDB("confirmed"); err != nil {
		t.Fatalf("SetStatusFromDB: %v", err)
	}
	if o.Status() != order.OrderStatusConfirmed {
		t.Errorf("Status = %q, want confirmed", o.Status())
	}
}

func TestSetStatusFromDB_Invalid(t *testing.T) {
	item := validItem(t)
	o, _ := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err := o.SetStatusFromDB("nope"); err == nil {
		t.Fatal("expected error for invalid status")
	}
}
