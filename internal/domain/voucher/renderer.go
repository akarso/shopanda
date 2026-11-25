package voucher

// PDFDocument is the input for a voucher pack PDF (one order, many codes).
type PDFDocument struct {
	OrderID    string
	CustomerID string
	IssuedAt   string
	Codes      []PDFCode
}

// PDFCode is one line on the pack.
type PDFCode struct {
	Code   string
	Status string
	Label  string // optional product/line label
}

// PDFRenderer generates a PDF document for issued vouchers.
type PDFRenderer interface {
	Render(doc PDFDocument) ([]byte, error)
}
