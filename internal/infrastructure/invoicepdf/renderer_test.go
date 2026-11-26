package invoicepdf_test

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf16"

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

	pdf, err := r.Render(inv, invoice.SellerBlock{})
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

func TestRenderer_Render_SellerBlockPolishUTF8(t *testing.T) {
	r := invoicepdf.NewRenderer()
	inv := testInvoice(t)
	seller := invoice.SellerBlock{
		LegalName:    "Spółka Łódź Gdańsk Kraków",
		AddressLines: []string{"Ul. Świętokrzyska 1", "00-001 Warszawa", "PL"},
		Phone:        "+48 22 000 00 00",
		Email:        "sales@acme.example",
		TaxIDs: []invoice.SellerTaxLine{
			{Label: "NIP", Value: "5252344022"},
			{Label: "EU VAT", Value: "PL5252344022"},
		},
	}

	pdf, err := r.Render(inv, seller)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("missing PDF magic")
	}
	text := extractUTF16PDFStrings(t, pdf)
	for _, want := range []string{"Spółka", "Łódź", "Gdańsk", "Kraków", "Świętokrzyska", "5252344022", "NIP"} {
		if !strings.Contains(text, want) {
			t.Errorf("extracted PDF text missing %q\n---\n%s\n---", want, text)
		}
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
	pdf, err := r.Render(inv, invoice.SellerBlock{})
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

func TestRenderer_Render_PageBreakKeepsRowAndTotals(t *testing.T) {
	price := shared.MustNewMoney(100, "EUR")
	items := make([]invoice.Item, 0, 40)
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("Wrapped product line %02d with a long Polish name Kraków Gdańsk Spółka enough to wrap", i)
		item, err := invoice.NewItem(fmt.Sprintf("v-%d", i), fmt.Sprintf("SKU-%02d", i), name, 1, price)
		if err != nil {
			t.Fatalf("NewItem %d: %v", i, err)
		}
		items = append(items, item)
	}
	tax := shared.MustNewMoney(920, "EUR")
	inv, err := invoice.NewInvoice("inv-pages", "ord-pages", "cust-1", "EUR", items, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	inv.SetInvoiceNumberFromDB(55)

	pdf, err := invoicepdf.NewRenderer().Render(inv, invoice.SellerBlock{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	text := extractUTF16PDFStrings(t, pdf)
	if !strings.Contains(text, "Wrapped product line 00") {
		t.Errorf("missing first row\n%s", text)
	}
	if !strings.Contains(text, "Wrapped product line 39") {
		t.Errorf("missing last row after page break\n%s", text)
	}
	if !strings.Contains(text, "Kraków") || !strings.Contains(text, "Spółka") {
		t.Errorf("wrapped name glyphs missing\n%s", text)
	}
	for _, want := range []string{"Subtotal", "Tax", "Total"} {
		if !strings.Contains(text, want) {
			t.Errorf("totals missing %q\n%s", want, text)
		}
	}
	// Table header should repeat on later pages.
	if strings.Count(text, "Unit Price") < 2 {
		t.Errorf("expected repeated table header across pages; Unit Price count too low\n%s", text)
	}
}

func TestRenderer_Render_LongItemNameWraps(t *testing.T) {
	longName := "Ręcznie szyte skórzane buty trekkingowe z membraną wodoodporną model Kraków-Gdańsk XL"
	price := shared.MustNewMoney(1000, "EUR")
	item, err := invoice.NewItem("v-1", "SKU-LONG-PRODUCT-CODE-001", longName, 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	tax := shared.MustNewMoney(230, "EUR")
	inv, err := invoice.NewInvoice("inv-long", "ord-long", "cust-1", "EUR", []invoice.Item{item}, tax)
	if err != nil {
		t.Fatalf("NewInvoice: %v", err)
	}
	inv.SetInvoiceNumberFromDB(7)

	pdf, err := invoicepdf.NewRenderer().Render(inv, invoice.SellerBlock{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	text := extractUTF16PDFStrings(t, pdf)
	for _, want := range []string{"Ręcznie", "Kraków", "Gdańsk", "SKU-LONG-PROD", "UCT-CODE-001"} {
		if !strings.Contains(text, want) {
			t.Errorf("extracted PDF text missing %q (truncation?)\n---\n%s\n---", want, text)
		}
	}
	// Wrapped SKU may insert a space at the break; compact before asserting full code.
	compact := strings.ReplaceAll(text, " ", "")
	if !strings.Contains(compact, "SKU-LONG-PRODUCT-CODE-001") {
		t.Errorf("full SKU missing after wrap\n---\n%s\n---", text)
	}
	if strings.Contains(text, "…") {
		t.Error("PDF must not truncate item names with ellipsis")
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
	pdf, err := r.Render(inv, invoice.SellerBlock{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Error("output does not start with PDF magic bytes")
	}
}

// extractUTF16PDFStrings inflates content streams and decodes fpdf UTF-8 font
// Tj literals (UTF-16BE inside PDF strings), so assertions check drawable text.
func extractUTF16PDFStrings(t *testing.T, pdf []byte) string {
	t.Helper()
	var out strings.Builder
	rest := pdf
	for {
		idx := bytes.Index(rest, []byte("stream"))
		if idx < 0 {
			break
		}
		start := idx + len("stream")
		if start < len(rest) && rest[start] == '\r' {
			start++
		}
		if start < len(rest) && rest[start] == '\n' {
			start++
		}
		end := bytes.Index(rest[start:], []byte("endstream"))
		if end < 0 {
			break
		}
		payload := rest[start : start+end]
		rest = rest[start+end+len("endstream"):]
		r, err := zlib.NewReader(bytes.NewReader(payload))
		if err != nil {
			continue
		}
		decoded, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil || len(decoded) > 50_000 {
			continue // skip font binary blobs
		}
		out.WriteString(decodePDFUTF16Literals(decoded))
		out.WriteByte('\n')
	}
	return out.String()
}

func decodePDFUTF16Literals(stream []byte) string {
	var b strings.Builder
	i := 0
	for i < len(stream) {
		if stream[i] != '(' {
			i++
			continue
		}
		i++
		var lit []byte
		for i < len(stream) {
			if stream[i] == ')' {
				i++
				break
			}
			if stream[i] == '\\' && i+1 < len(stream) {
				i++
				lit = append(lit, stream[i])
				i++
				continue
			}
			lit = append(lit, stream[i])
			i++
		}
		if len(lit) >= 2 && len(lit)%2 == 0 && lit[0] == 0 {
			u16 := make([]uint16, 0, len(lit)/2)
			for j := 0; j+1 < len(lit); j += 2 {
				u16 = append(u16, uint16(lit[j])<<8|uint16(lit[j+1]))
			}
			b.WriteString(string(utf16.Decode(u16)))
			b.WriteByte(' ')
		}
	}
	return b.String()
}
