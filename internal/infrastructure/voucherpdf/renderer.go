package voucherpdf

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"github.com/go-pdf/fpdf"

	"github.com/akarso/shopanda/internal/domain/voucher"
)

var _ voucher.PDFRenderer = (*Renderer)(nil)

// Renderer generates voucher pack PDFs using fpdf.
type Renderer struct{}

// NewRenderer creates a Renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// asciiPDFString keeps Helvetica/cp1252-safe text so Render never fails on
// non-encodable order or customer ids.
func asciiPDFString(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (unicode.IsPrint(r) || r == ' ') {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// Render generates a PDF listing the issued codes for an order.
func (r *Renderer) Render(doc voucher.PDFDocument) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 20)
	pdf.Cell(0, 10, "VOUCHER")
	pdf.Ln(14)

	pdf.SetFont("Helvetica", "", 10)
	orderLabel := asciiPDFString(doc.OrderID)
	if orderLabel == "" {
		orderLabel = "-"
	}
	pdf.Cell(0, 6, fmt.Sprintf("Order: %s", orderLabel))
	pdf.Ln(6)
	if cust := asciiPDFString(doc.CustomerID); cust != "" {
		pdf.Cell(0, 6, fmt.Sprintf("Customer: %s", cust))
		pdf.Ln(6)
	}
	if issued := asciiPDFString(doc.IssuedAt); issued != "" {
		pdf.Cell(0, 6, fmt.Sprintf("Issued: %s", issued))
		pdf.Ln(6)
	}
	pdf.Ln(6)

	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(80, 8, "Code", "1", 0, "", false, 0, "")
	pdf.CellFormat(40, 8, "Status", "1", 0, "", false, 0, "")
	pdf.CellFormat(60, 8, "Label", "1", 0, "", false, 0, "")
	pdf.Ln(8)

	pdf.SetFont("Courier", "B", 12)
	for _, c := range doc.Codes {
		code := voucher.FormatCode(c.Code)
		pdf.SetFont("Courier", "B", 12)
		pdf.CellFormat(80, 10, code, "1", 0, "", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(40, 10, asciiPDFString(c.Status), "1", 0, "", false, 0, "")
		pdf.CellFormat(60, 10, asciiPDFString(c.Label), "1", 0, "", false, 0, "")
		pdf.Ln(10)
	}

	if len(doc.Codes) == 0 {
		pdf.SetFont("Helvetica", "I", 10)
		pdf.Cell(0, 8, "No codes")
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("voucherpdf: output: %w", err)
	}
	return buf.Bytes(), nil
}
