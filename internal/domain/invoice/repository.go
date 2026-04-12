package invoice

import "context"

// InvoiceRepository defines persistence operations for invoices.
type InvoiceRepository interface {
	// FindByID returns an invoice with its items by ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Invoice, error)

	// FindByOrderID returns the invoice for an order.
	// Returns (nil, nil) when not found.
	FindByOrderID(ctx context.Context, orderID string) (*Invoice, error)

	// Save persists an invoice and its items (insert-only).
	// Assigns InvoiceNumber from the DB sequence.
	Save(ctx context.Context, inv *Invoice) error
}

// CreditNoteRepository defines persistence operations for credit notes.
type CreditNoteRepository interface {
	// FindByID returns a credit note with its items by ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*CreditNote, error)

	// FindByInvoiceID returns all credit notes for an invoice, newest first.
	FindByInvoiceID(ctx context.Context, invoiceID string) ([]CreditNote, error)

	// Save persists a credit note and its items (insert-only).
	// Assigns CreditNoteNumber from the DB sequence.
	Save(ctx context.Context, cn *CreditNote) error
}
