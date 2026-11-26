package invoicepdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/akarso/shopanda/internal/domain/invoice"
)

//go:embed fonts/DejaVuSans.ttf
var dejaVuSans []byte

//go:embed fonts/DejaVuSans-Bold.ttf
var dejaVuSansBold []byte

const (
	fontFamily = "DejaVu"
	pageWidth  = 210.0 // A4 mm
	leftMargin = 10.0
	textWidth  = pageWidth - 2*leftMargin
)

// Compile-time check.
var _ invoice.PDFRenderer = (*Renderer)(nil)

// Renderer generates invoice PDFs using fpdf with an embedded UTF-8 font.
type Renderer struct{}

// NewRenderer creates a Renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// Render generates a single-page PDF for the given invoice.
// Seller must be the sealed snapshot from the invoice (may be empty).
func (r *Renderer) Render(inv invoice.Invoice, seller invoice.SellerBlock) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(true)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddUTF8FontFromBytes(fontFamily, "", dejaVuSans)
	pdf.AddUTF8FontFromBytes(fontFamily, "B", dejaVuSansBold)
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("invoicepdf: load font: %w", err)
	}
	pdf.AddPage()

	seller = seller.SanitizeForPDF()

	// ── Header ──────────────────────────────────────────────────────
	pdf.SetFont(fontFamily, "B", 20)
	writeWrapped(pdf, "INVOICE", 10)
	pdf.Ln(4)

	// ── Seller block (sealed snapshot) ──────────────────────────────
	if !seller.IsEmpty() {
		pdf.SetFont(fontFamily, "B", 11)
		writeWrapped(pdf, "Seller", 6)
		pdf.SetFont(fontFamily, "", 10)
		if name := strings.TrimSpace(seller.LegalName); name != "" {
			writeWrapped(pdf, name, 5)
		}
		for _, line := range seller.AddressLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			writeWrapped(pdf, line, 5)
		}
		for _, tax := range seller.TaxIDs {
			value := strings.TrimSpace(tax.Value)
			if value == "" {
				continue
			}
			label := strings.TrimSpace(tax.Label)
			text := value
			if label != "" {
				text = label + ": " + value
			}
			writeWrapped(pdf, text, 5)
		}
		if phone := strings.TrimSpace(seller.Phone); phone != "" {
			writeWrapped(pdf, "Phone: "+phone, 5)
		}
		if email := strings.TrimSpace(seller.Email); email != "" {
			writeWrapped(pdf, "Email: "+email, 5)
		}
		pdf.Ln(4)
	}

	// ── Metadata ────────────────────────────────────────────────────
	pdf.SetFont(fontFamily, "", 10)
	writeWrapped(pdf, fmt.Sprintf("Invoice #: %d", inv.InvoiceNumber()), 6)
	writeWrapped(pdf, fmt.Sprintf("Date: %s", inv.CreatedAt().Format("2006-01-02")), 6)
	writeWrapped(pdf, fmt.Sprintf("Order: %s", inv.OrderID()), 6)
	writeWrapped(pdf, fmt.Sprintf("Customer: %s", inv.CustomerID()), 6)
	pdf.Ln(6)

	// ── Items table header ──────────────────────────────────────────
	pdf.SetFont(fontFamily, "B", 9)
	pdf.CellFormat(60, 8, "Item", "1", 0, "", false, 0, "")
	pdf.CellFormat(30, 8, "SKU", "1", 0, "", false, 0, "")
	pdf.CellFormat(20, 8, "Qty", "1", 0, "C", false, 0, "")
	pdf.CellFormat(35, 8, "Unit Price", "1", 0, "R", false, 0, "")
	pdf.CellFormat(35, 8, "Total", "1", 0, "R", false, 0, "")
	pdf.Ln(8)

	// ── Items rows ──────────────────────────────────────────────────
	pdf.SetFont(fontFamily, "", 9)
	cur := inv.Currency()
	for _, item := range inv.Items() {
		lt, err := item.LineTotal()
		if err != nil {
			return nil, fmt.Errorf("invoicepdf: line total: %w", err)
		}
		writeItemRow(pdf, item.Name, item.SKU, item.Quantity, formatMoney(item.UnitPrice.Amount(), cur), formatMoney(lt.Amount(), cur))
	}

	// ── Totals ──────────────────────────────────────────────────────
	pdf.Ln(5)
	pdf.SetFont(fontFamily, "", 10)
	const totalsX = 145.0

	pdf.SetX(totalsX)
	pdf.CellFormat(20, 7, "Subtotal:", "", 0, "R", false, 0, "")
	pdf.CellFormat(25, 7, formatMoney(inv.SubtotalAmount().Amount(), cur), "", 0, "R", false, 0, "")
	pdf.Ln(7)

	pdf.SetX(totalsX)
	pdf.CellFormat(20, 7, "Tax:", "", 0, "R", false, 0, "")
	pdf.CellFormat(25, 7, formatMoney(inv.TaxAmount().Amount(), cur), "", 0, "R", false, 0, "")
	pdf.Ln(7)

	pdf.SetFont(fontFamily, "B", 10)
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

func writeWrapped(pdf *fpdf.Fpdf, text string, lineHt float64) {
	pdf.MultiCell(textWidth, lineHt, text, "", "", false)
}

const (
	colName  = 60.0
	colSKU   = 30.0
	colQty   = 20.0
	colUnit  = 35.0
	colTotal = 35.0
	rowLine  = 5.0
)

// writeItemRow draws one invoice line with wrapped name/SKU (no truncation).
func writeItemRow(pdf *fpdf.Fpdf, name, sku string, qty int, unitPrice, lineTotal string) {
	name = strings.TrimSpace(name)
	sku = strings.TrimSpace(sku)
	nameLines := pdf.SplitText(name, colName-1)
	skuLines := pdf.SplitText(sku, colSKU-1)
	n := len(nameLines)
	if len(skuLines) > n {
		n = len(skuLines)
	}
	if n < 1 {
		n = 1
		nameLines = []string{""}
	}
	rowH := float64(n) * rowLine
	x, y := pdf.GetXY()

	pdf.Rect(x, y, colName, rowH, "D")
	pdf.Rect(x+colName, y, colSKU, rowH, "D")
	pdf.Rect(x+colName+colSKU, y, colQty, rowH, "D")
	pdf.Rect(x+colName+colSKU+colQty, y, colUnit, rowH, "D")
	pdf.Rect(x+colName+colSKU+colQty+colUnit, y, colTotal, rowH, "D")

	for i, line := range nameLines {
		pdf.SetXY(x+1, y+float64(i)*rowLine)
		pdf.CellFormat(colName-2, rowLine, line, "", 0, "", false, 0, "")
	}
	for i, line := range skuLines {
		pdf.SetXY(x+colName+1, y+float64(i)*rowLine)
		pdf.CellFormat(colSKU-2, rowLine, line, "", 0, "", false, 0, "")
	}
	pdf.SetXY(x+colName+colSKU, y)
	pdf.CellFormat(colQty, rowH, fmt.Sprintf("%d", qty), "", 0, "C", false, 0, "")
	pdf.CellFormat(colUnit, rowH, unitPrice, "", 0, "R", false, 0, "")
	pdf.CellFormat(colTotal, rowH, lineTotal, "", 0, "R", false, 0, "")
	pdf.SetXY(x, y+rowH)
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
