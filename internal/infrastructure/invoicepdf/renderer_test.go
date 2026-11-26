package invoicepdf_test

import (
	"bytes"
	"compress/zlib"
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
