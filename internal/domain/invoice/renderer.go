package invoice

// PDFRenderer generates a PDF document from an invoice.
type PDFRenderer interface {
	Render(inv Invoice) ([]byte, error)
}
