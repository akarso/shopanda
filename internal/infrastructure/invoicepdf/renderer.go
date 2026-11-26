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
	fontFamily  = "DejaVu"
	pageWidth   = 210.0 // A4 mm
	leftMargin  = 10.0
	textWidth   = pageWidth - 2*leftMargin
	breakMargin = 20.0
)

// Compile-time check.
var _ invoice.PDFRenderer = (*Renderer)(nil)

// Renderer generates invoice PDFs using fpdf with an embedded UTF-8 font.
type Renderer struct{}

// NewRenderer creates a Renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// Render generates a PDF for the given invoice (may span pages).
// Seller must be the sealed snapshot from the invoice (may be empty).
func (r *Renderer) Render(inv invoice.Invoice, seller invoice.SellerBlock) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(true)
	pdf.SetMargins(leftMargin, 10, leftMargin)
	pdf.SetAutoPageBreak(true, breakMargin)
	pdf.AddUTF8FontFromBytes(fontFamily, "", dejaVuSans)
	pdf.AddUTF8FontFromBytes(fontFamily, "B", dejaVuSansBold)
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("invoicepdf: load font: %w", err)
	}
	pdf.AddPage()

	seller = seller.SanitizeForPDF()

	pdf.SetFont(fontFamily, "B", 20)
	writeWrapped(pdf, "INVOICE", 10)
	pdf.Ln(4)

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

	pdf.SetFont(fontFamily, "", 10)
	writeWrapped(pdf, fmt.Sprintf("Invoice #: %d", inv.InvoiceNumber()), 6)
	writeWrapped(pdf, fmt.Sprintf("Date: %s", inv.CreatedAt().Format("2006-01-02")), 6)
	writeWrapped(pdf, fmt.Sprintf("Order: %s", inv.OrderID()), 6)
	writeWrapped(pdf, fmt.Sprintf("Customer: %s", inv.CustomerID()), 6)
	pdf.Ln(6)

	pdf.SetAutoPageBreak(false, 0)

	items := inv.Items()
	cur := inv.Currency()
	tableStarted := false
	for _, item := range items {
		lt, err := item.LineTotal()
		if err != nil {
			return nil, fmt.Errorf("invoicepdf: line total: %w", err)
		}
		unit := formatMoney(item.UnitPrice.Amount(), cur)
		total := formatMoney(lt.Amount(), cur)
		if !tableStarted {
			// Reserve space for header + at least one text line so the header
			// is never left alone in the bottom margin.
			if err := ensureSpace(pdf, headerH+rowLine); err != nil {
				return nil, err
			}
			writeTableHeader(pdf)
			tableStarted = true
		}
		pdf.SetFont(fontFamily, "", 9)
		if err := writeItemRow(pdf, item.Name, item.SKU, item.Quantity, unit, total); err != nil {
			return nil, err
		}
	}

	if err := ensureSpace(pdf, 5+7*3); err != nil {
		return nil, err
	}
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
	headerH  = 8.0
)

func writeTableHeader(pdf *fpdf.Fpdf) {
	pdf.SetFont(fontFamily, "B", 9)
	pdf.CellFormat(colName, headerH, "Item", "1", 0, "", false, 0, "")
	pdf.CellFormat(colSKU, headerH, "SKU", "1", 0, "", false, 0, "")
	pdf.CellFormat(colQty, headerH, "Qty", "1", 0, "C", false, 0, "")
	pdf.CellFormat(colUnit, headerH, "Unit Price", "1", 0, "R", false, 0, "")
	pdf.CellFormat(colTotal, headerH, "Total", "1", 0, "R", false, 0, "")
	pdf.Ln(headerH)
}

func pageBottom(pdf *fpdf.Fpdf) float64 {
	_, pageH := pdf.GetPageSize()
	return pageH - breakMargin
}

func maxBodyLines(pdf *fpdf.Fpdf) int {
	_, top, _, _ := pdf.GetMargins()
	maxH := pageBottom(pdf) - top - headerH
	n := int(maxH / rowLine)
	if n < 1 {
		return 1
	}
	return n
}

func ensureSpace(pdf *fpdf.Fpdf, need float64) error {
	_, y := pdf.GetXY()
	if y+need <= pageBottom(pdf) {
		return nil
	}
	pdf.AddPage()
	return nil
}

// writeItemRow draws one invoice line. Quantity and amounts stay on the first
// fragment only; overflow continues as name/SKU text (not a second priced row).
func writeItemRow(pdf *fpdf.Fpdf, name, sku string, qty int, unitPrice, lineTotal string) error {
	name = strings.TrimSpace(name)
	sku = strings.TrimSpace(sku)
	nameLines := pdf.SplitText(name, colName-1)
	skuLines := pdf.SplitText(sku, colSKU-1)
	if len(nameLines) == 0 {
		nameLines = []string{""}
	}
	totalLines := len(nameLines)
	if len(skuLines) > totalLines {
		totalLines = len(skuLines)
	}

	maxLines := maxBodyLines(pdf)
	offset := 0
	first := true
	for offset < totalLines {
		_, y := pdf.GetXY()
		room := int((pageBottom(pdf) - y) / rowLine)
		if room < 1 {
			if err := ensureSpace(pdf, headerH+rowLine); err != nil {
				return err
			}
			writeTableHeader(pdf)
			pdf.SetFont(fontFamily, "", 9)
			_, y = pdf.GetXY()
			room = int((pageBottom(pdf) - y) / rowLine)
			if room < 1 {
				room = 1
			}
		}
		chunk := room
		if chunk > maxLines {
			chunk = maxLines
		}
		remain := totalLines - offset
		if chunk > remain {
			chunk = remain
		}
		chunkH := float64(chunk) * rowLine
		if y+chunkH > pageBottom(pdf) {
			if err := ensureSpace(pdf, headerH+chunkH); err != nil {
				return err
			}
			writeTableHeader(pdf)
			pdf.SetFont(fontFamily, "", 9)
		}

		nameChunk := sliceLines(nameLines, offset, chunk)
		skuChunk := sliceLines(skuLines, offset, chunk)
		if first {
			drawItemChunk(pdf, nameChunk, skuChunk, fmt.Sprintf("%d", qty), unitPrice, lineTotal, chunkH)
			first = false
		} else {
			drawItemContinuation(pdf, nameChunk, skuChunk, chunkH)
		}
		offset += chunk
	}
	return nil
}

func sliceLines(lines []string, offset, n int) []string {
	if offset >= len(lines) {
		out := make([]string, n)
		return out
	}
	end := offset + n
	if end > len(lines) {
		end = len(lines)
	}
	out := make([]string, n)
	copy(out, lines[offset:end])
	return out
}

func drawItemChunk(pdf *fpdf.Fpdf, nameLines, skuLines []string, qty, unitPrice, lineTotal string, rowH float64) {
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
	pdf.CellFormat(colQty, rowH, qty, "", 0, "C", false, 0, "")
	pdf.CellFormat(colUnit, rowH, unitPrice, "", 0, "R", false, 0, "")
	pdf.CellFormat(colTotal, rowH, lineTotal, "", 0, "R", false, 0, "")
	pdf.SetXY(x, y+rowH)
}

// drawItemContinuation continues a wrapped name/SKU without empty amount cells
// (those would look like another line item).
func drawItemContinuation(pdf *fpdf.Fpdf, nameLines, skuLines []string, rowH float64) {
	x, y := pdf.GetXY()
	pdf.Rect(x, y, colName, rowH, "D")
	pdf.Rect(x+colName, y, colSKU, rowH, "D")
	for i, line := range nameLines {
		pdf.SetXY(x+1, y+float64(i)*rowLine)
		pdf.CellFormat(colName-2, rowLine, line, "", 0, "", false, 0, "")
	}
	for i, line := range skuLines {
		pdf.SetXY(x+colName+1, y+float64(i)*rowLine)
		pdf.CellFormat(colSKU-2, rowLine, line, "", 0, "", false, 0, "")
	}
	pdf.SetXY(x, y+rowH)
}

func formatMoney(amount int64, currency string) string {
	whole := amount / 100
	cents := amount % 100
	if cents < 0 {
		cents = -cents
	}
	return fmt.Sprintf("%s %d.%02d", currency, whole, cents)
}
