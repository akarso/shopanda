package invoicepdf

import (
	"bytes"
	"fmt"

	"github.com/go-pdf/fpdf"

	"github.com/akarso/shopanda/internal/domain/invoice"
)

// Compile-time check.
var _ invoice.PDFRenderer = (*Renderer)(nil)

// Renderer generates invoice PDFs using fpdf.
type Renderer struct{}

// NewRenderer creates a Renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// Render generates a single-page PDF for the given invoice.
func (r *Renderer) Render(inv invoice.Invoice) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()

	// ── Header ──────────────────────────────────────────────────────
	pdf.SetFont("Helvetica", "B", 20)
	pdf.Cell(0, 10, "INVOICE")
	pdf.Ln(15)

	// ── Metadata ────────────────────────────────────────────────────
	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(0, 6, fmt.Sprintf("Invoice #: %d", inv.InvoiceNumber()))
	pdf.Ln(6)
	pdf.Cell(0, 6, fmt.Sprintf("Date: %s", inv.CreatedAt().Format("2006-01-02")))
	pdf.Ln(6)
	pdf.Cell(0, 6, fmt.Sprintf("Order: %s", inv.OrderID()))
	pdf.Ln(6)
	pdf.Cell(0, 6, fmt.Sprintf("Customer: %s", inv.CustomerID()))
	pdf.Ln(12)

	// ── Items table header ──────────────────────────────────────────
	pdf.SetFont("Helvetica", "B", 9)
	pdf.CellFormat(60, 8, "Item", "1", 0, "", false, 0, "")
	pdf.CellFormat(30, 8, "SKU", "1", 0, "", false, 0, "")
	pdf.CellFormat(20, 8, "Qty", "1", 0, "C", false, 0, "")
	pdf.CellFormat(35, 8, "Unit Price", "1", 0, "R", false, 0, "")
	pdf.CellFormat(35, 8, "Total", "1", 0, "R", false, 0, "")
	pdf.Ln(8)

	// ── Items rows ──────────────────────────────────────────────────
	pdf.SetFont("Helvetica", "", 9)
	cur := inv.Currency()
	for _, item := range inv.Items() {
		lt, err := item.LineTotal()
		if err != nil {
			return nil, fmt.Errorf("invoicepdf: line total: %w", err)
		}
		pdf.CellFormat(60, 7, item.Name, "1", 0, "", false, 0, "")
		pdf.CellFormat(30, 7, item.SKU, "1", 0, "", false, 0, "")
		pdf.CellFormat(20, 7, fmt.Sprintf("%d", item.Quantity), "1", 0, "C", false, 0, "")
		pdf.CellFormat(35, 7, formatMoney(item.UnitPrice.Amount(), cur), "1", 0, "R", false, 0, "")
		pdf.CellFormat(35, 7, formatMoney(lt.Amount(), cur), "1", 0, "R", false, 0, "")
		pdf.Ln(7)
	}

	// ── Totals ──────────────────────────────────────────────────────
	pdf.Ln(5)
	pdf.SetFont("Helvetica", "", 10)
	const totalsX = 145.0

	pdf.SetX(totalsX)
	pdf.CellFormat(20, 7, "Subtotal:", "", 0, "R", false, 0, "")
	pdf.CellFormat(25, 7, formatMoney(inv.SubtotalAmount().Amount(), cur), "", 0, "R", false, 0, "")
	pdf.Ln(7)

	pdf.SetX(totalsX)
	pdf.CellFormat(20, 7, "Tax:", "", 0, "R", false, 0, "")
	pdf.CellFormat(25, 7, formatMoney(inv.TaxAmount().Amount(), cur), "", 0, "R", false, 0, "")
	pdf.Ln(7)

	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetX(totalsX)
	pdf.CellFormat(20, 7, "Total:", "", 0, "R", false, 0, "")
	pdf.CellFormat(25, 7, formatMoney(inv.TotalAmount().Amount(), cur), "", 0, "R", false, 0, "")

	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("invoicepdf: build: %w", err)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("invoicepdf: write: %w", err)
	}
	return buf.Bytes(), nil
}

// formatMoney formats an int64 minor-unit amount as "CUR major.minor".
func formatMoney(amount int64, currency string) string {
	whole := amount / 100
	cents := amount % 100
	if cents < 0 {
		cents = -cents
	}
	return fmt.Sprintf("%s %d.%02d", currency, whole, cents)
}
