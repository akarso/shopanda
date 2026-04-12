package invoice_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ── NewCreditNote ───────────────────────────────────────────────────────

func TestNewCreditNote_Success(t *testing.T) {
	item := validItem(t)
	tax := validTax()
	cn, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "damaged goods", "EUR", []invoice.Item{item}, tax)
	if err != nil {
		t.Fatalf("NewCreditNote: %v", err)
	}
	if cn.InvoiceID != "inv-1" {
		t.Errorf("InvoiceID = %q, want inv-1", cn.InvoiceID)
	}
	if cn.OrderID != "ord-1" {
		t.Errorf("OrderID = %q, want ord-1", cn.OrderID)
	}
	if cn.Reason != "damaged goods" {
		t.Errorf("Reason = %q, want damaged goods", cn.Reason)
	}
	// subtotal = 1000 * 2 = 2000
	if cn.SubtotalAmount.Amount() != 2000 {
		t.Errorf("SubtotalAmount = %d, want 2000", cn.SubtotalAmount.Amount())
	}
	if cn.TaxAmount.Amount() != 380 {
		t.Errorf("TaxAmount = %d, want 380", cn.TaxAmount.Amount())
	}
	// total = 2000 + 380 = 2380
	if cn.TotalAmount.Amount() != 2380 {
		t.Errorf("TotalAmount = %d, want 2380", cn.TotalAmount.Amount())
	}
	if cn.CreditNoteNumber != 0 {
		t.Errorf("CreditNoteNumber = %d, want 0 (assigned on save)", cn.CreditNoteNumber)
	}
}

func TestNewCreditNote_EmptyID(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewCreditNote("", "inv-1", "ord-1", "cust-1", "reason", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewCreditNote_EmptyInvoiceID(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewCreditNote(id.New(), "", "ord-1", "cust-1", "reason", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty invoice id")
	}
}

func TestNewCreditNote_EmptyOrderID(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewCreditNote(id.New(), "inv-1", "", "cust-1", "reason", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestNewCreditNote_EmptyCustomerID(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "", "reason", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty customer id")
	}
}

func TestNewCreditNote_EmptyReason(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "", "EUR", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for empty reason")
	}
}

func TestNewCreditNote_InvalidCurrency(t *testing.T) {
	item := validItem(t)
	_, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "reason", "xx", []invoice.Item{item}, validTax())
	if err == nil {
		t.Fatal("expected error for invalid currency")
	}
}

func TestNewCreditNote_NoItems(t *testing.T) {
	_, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "reason", "EUR", nil, validTax())
	if err == nil {
		t.Fatal("expected error for no items")
	}
}

func TestNewCreditNote_TaxCurrencyMismatch(t *testing.T) {
	item := validItem(t)
	usdTax := shared.MustNewMoney(100, "USD")
	_, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "reason", "EUR", []invoice.Item{item}, usdTax)
	if err == nil {
		t.Fatal("expected error for tax currency mismatch")
	}
}

func TestNewCreditNote_NegativeTax(t *testing.T) {
	item := validItem(t)
	negTax := shared.MustNewMoney(-100, "EUR")
	_, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "reason", "EUR", []invoice.Item{item}, negTax)
	if err == nil {
		t.Fatal("expected error for negative tax")
	}
}

func TestCreditNote_DefensiveCopy(t *testing.T) {
	item := validItem(t)
	cn, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "reason", "EUR", []invoice.Item{item}, validTax())
	if err != nil {
		t.Fatalf("NewCreditNote: %v", err)
	}
	items := cn.Items()
	items[0].Name = "MUTATED"
	if cn.Items()[0].Name == "MUTATED" {
		t.Error("Items() must return a defensive copy")
	}
}

// ── SetItemsFromDB ──────────────────────────────────────────────────────

func TestCreditNote_SetItemsFromDB_MismatchSubtotal(t *testing.T) {
	item := validItem(t)
	cn, err := invoice.NewCreditNote(id.New(), "inv-1", "ord-1", "cust-1", "reason", "EUR", []invoice.Item{item}, validTax())
	if err != nil {
		t.Fatalf("NewCreditNote: %v", err)
	}
	wrongPrice := shared.MustNewMoney(9999, "EUR")
	wrongItem, err := invoice.NewItem("v-x", "SKU-X", "Other", 1, wrongPrice)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	err = cn.SetItemsFromDB([]invoice.Item{wrongItem})
	if err == nil {
		t.Fatal("expected error for subtotal mismatch")
	}
}
