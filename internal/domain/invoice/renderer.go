package invoice

// PDFRenderer generates a PDF document from an invoice.
// Pass an empty SellerBlock to omit the seller identity section.
type PDFRenderer interface {
	Render(inv Invoice, seller SellerBlock) ([]byte, error)
}
