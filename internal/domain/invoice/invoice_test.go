package invoice_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

func validItem(t *testing.T) invoice.Item {
	t.Helper()
	price := shared.MustNewMoney(1000, "EUR")
	item, err := invoice.NewItem("variant-1", "SKU-001", "Blue Shirt", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	return item
}

func validTax() shared.Money {
	return shared.MustNewMoney(380, "EUR")
}

// ── NewInvoice ──────────────────────────────────────────────────────────

func TestNewInvoice_Success(t *testing.T) {
	item := validItem(t)
	tax := validTax()
	inv, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	if inv.Status() != invoice.InvoiceStatusIssued {
		t.Errorf("Status = %q, want issued", inv.Status())
	}
	if inv.OrderID() != "ord-1" {
		t.Errorf("OrderID = %q, want ord-1", inv.OrderID())
	}
	if inv.CustomerID() != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", inv.CustomerID())
	}
	// subtotal = 1000 * 2 = 2000
	if inv.SubtotalAmount().Amount() != 2000 {
		t.Errorf("SubtotalAmount = %d, want 2000", inv.SubtotalAmount().Amount())
	}
	if inv.TaxAmount().Amount() != 380 {
		t.Errorf("TaxAmount = %d, want 380", inv.TaxAmount().Amount())
	}
	// total = 2000 + 380 = 2380
	if inv.TotalAmount().Amount() != 2380 {
		t.Errorf("TotalAmount = %d, want 2380", inv.TotalAmount().Amount())
	}
	if len(inv.Items()) != 1 {
		t.Errorf("Items = %d, want 1", len(inv.Items()))
	}
	if inv.InvoiceNumber() != 0 {
		t.Errorf("InvoiceNumber = %d, want 0 (assigned on save)", inv.InvoiceNumber())
	}
}

func TestNewInvoice_EmptyID(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewInvoice("", "ord-1", "cust-1", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewInvoice_EmptyOrderID(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewInvoice(id.New(), "", "cust-1", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestNewInvoice_EmptyCustomerID(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewInvoice(id.New(), "ord-1", "", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty customer id")
	}
}

func TestNewInvoice_InvalidCurrency(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "xx", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for invalid currency")
	}
}

func TestNewInvoice_NoItems(t *testing.T) {
	_, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", nil, validTax())
	if err == nil {
		t.Fatal("expected error for no items")
	}
}

func TestNewInvoice_TaxCurrencyMismatch(t *testing.T) {
	item := validItem(t)
	usdTax := shared.MustNewMoney(100, "USD")
	_, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, usdTax)
	if err == nil {
		t.Fatal("expected error for tax currency mismatch")
	}
}

func TestNewInvoice_NegativeTax(t *testing.T) {
	item := validItem(t)
	negTax := shared.MustNewMoney(-100, "EUR")
	_, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, negTax)
	if err == nil {
		t.Fatal("expected error for negative tax")
	}
}

func TestNewInvoice_ItemCurrencyMismatch(t *testing.T) {
	usdPrice := shared.MustNewMoney(1000, "USD")
	item, err := invoice.NewItem("v-1", "SKU", "Shirt", 1, usdPrice)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	_, err = invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for item currency mismatch")
	}
}

func TestNewInvoice_MultipleItems(t *testing.T) {
	p1 := shared.MustNewMoney(1000, "EUR")
	p2 := shared.MustNewMoney(500, "EUR")
	i1, err := invoice.NewItem("v-1", "SKU-1", "Shirt", 2, p1)
	if err != nil {
		t.Fatalf("NewItem i1: %v", err)
	}
	i2, err := invoice.NewItem("v-2", "SKU-2", "Hat", 3, p2)
	if err != nil {
		t.Fatalf("NewItem i2: %v", err)
	}
	tax := shared.MustNewMoney(700, "EUR")
	inv, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{i1, i2}, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	// subtotal = 1000*2 + 500*3 = 3500
	if inv.SubtotalAmount().Amount() != 3500 {
		t.Errorf("SubtotalAmount = %d, want 3500", inv.SubtotalAmount().Amount())
	}
	// total = 3500 + 700 = 4200
	if inv.TotalAmount().Amount() != 4200 {
		t.Errorf("TotalAmount = %d, want 4200", inv.TotalAmount().Amount())
	}
}

func TestInvoice_DefensiveCopy(t *testing.T) {
	item := validItem(t)
	inv, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, validTax())
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	items := inv.Items()
	items[0].Name = "MUTATED"
	if inv.Items()[0].Name == "MUTATED" {
		t.Error("Items() must return a defensive copy")
	}
}

// ── InvoiceStatus ───────────────────────────────────────────────────────

func TestInvoiceStatus_IsValid(t *testing.T) {
	cases := []struct {
		status invoice.InvoiceStatus
		want   bool
	}{
		{invoice.InvoiceStatusIssued, true},
		{"bogus", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tc.status.IsValid(); got != tc.want {
			t.Errorf("InvoiceStatus(%q).IsValid() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// ── SetStatusFromDB ─────────────────────────────────────────────────────

func TestInvoice_SetStatusFromDB_Valid(t *testing.T) {
	item := validItem(t)
	inv, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, validTax())
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	if err := inv.SetStatusFromDB("issued"); err != nil {
		t.Fatalf("SetStatusFromDB: %v", err)
	}
	if inv.Status() != invoice.InvoiceStatusIssued {
		t.Errorf("Status = %q, want issued", inv.Status())
	}
}

func TestInvoice_SetStatusFromDB_Invalid(t *testing.T) {
	item := validItem(t)
	inv, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, validTax())
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	if err := inv.SetStatusFromDB("bogus"); err == nil {
		t.Fatal("expected error for invalid status")
	}
}

// ── SetItemsFromDB ──────────────────────────────────────────────────────

func TestInvoice_SetItemsFromDB_MismatchSubtotal(t *testing.T) {
	item := validItem(t)
	inv, err := invoice.NewInvoice(id.New(), "ord-1", "cust-1", "EUR", []invoice.Item{item}, validTax())
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	// create items with different subtotal
	wrongPrice := shared.MustNewMoney(9999, "EUR")
	wrongItem, err := invoice.NewItem("v-x", "SKU-X", "Other", 1, wrongPrice)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	err = inv.SetItemsFromDB([]invoice.Item{wrongItem})
	if err == nil {
		t.Fatal("expected error for subtotal mismatch")
	}
}
