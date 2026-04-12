package invoice

const (
	EventInvoiceCreated    = "invoice.created"
	EventCreditNoteCreated = "credit_note.created"
)

// InvoiceCreatedData is the event payload when an invoice is created.
type InvoiceCreatedData struct {
	InvoiceID     string `json:"invoice_id"`
	InvoiceNumber int64  `json:"invoice_number"`
	OrderID       string `json:"order_id"`
	CustomerID    string `json:"customer_id"`
	Currency      string `json:"currency"`
}

// CreditNoteCreatedData is the event payload when a credit note is created.
type CreditNoteCreatedData struct {
	CreditNoteID     string `json:"credit_note_id"`
	CreditNoteNumber int64  `json:"credit_note_number"`
	InvoiceID        string `json:"invoice_id"`
	OrderID          string `json:"order_id"`
	CustomerID       string `json:"customer_id"`
	Reason           string `json:"reason"`
}
