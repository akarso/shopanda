package invoicepdf_test

import (
	"bytes"
	"testing"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/invoicepdf"
)

func testInvoice(t *testing.T) invoice.Invoice {
	t.Helper()
	price := shared.MustNewMoney(1000, "EUR")
	item, err := invoice.NewItem("v-1", "SKU-001", "Blue Shirt", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	tax := shared.MustNewMoney(380, "EUR")
	inv, err := invoice.NewInvoice("inv-1", "ord-1", "cust-1", "EUR", []invoice.Item{item}, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	inv.SetInvoiceNumberFromDB(42)
	return inv
}

func TestRenderer_Render_Success(t *testing.T) {
	r := invoicepdf.NewRenderer()
	inv := testInvoice(t)

	pdf, err := r.Render(inv)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(pdf) == 0 {
		t.Fatal("expected non-empty PDF output")
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Error("output does not start with PDF magic bytes")
	}
}

func TestRenderer_Render_MultipleItems(t *testing.T) {
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
	inv, err := invoice.NewInvoice("inv-2", "ord-2", "cust-2", "EUR", []invoice.Item{i1, i2}, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	inv.SetInvoiceNumberFromDB(99)

	r := invoicepdf.NewRenderer()
	pdf, err := r.Render(inv)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Error("output does not start with PDF magic bytes")
	}
	if len(pdf) < 100 {
		t.Errorf("PDF suspiciously small: %d bytes", len(pdf))
	}
}

func TestRenderer_Render_ZeroTax(t *testing.T) {
	price := shared.MustNewMoney(500, "USD")
	item, err := invoice.NewItem("v-1", "SKU-1", "Widget", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	zeroTax := shared.MustNewMoney(0, "USD")
	inv, err := invoice.NewInvoice("inv-3", "ord-3", "cust-3", "USD", []invoice.Item{item}, zeroTax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	inv.SetInvoiceNumberFromDB(1)

	r := invoicepdf.NewRenderer()
	pdf, err := r.Render(inv)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Error("output does not start with PDF magic bytes")
	}
}
