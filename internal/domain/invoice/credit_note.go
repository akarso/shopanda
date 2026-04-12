package invoice

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// CreditNote is an immutable correction document linked to an invoice.
type CreditNote struct {
	ID               string
	CreditNoteNumber int64 // assigned by DB sequence on save
	InvoiceID        string
	OrderID          string
	CustomerID       string
	Reason           string
	Currency         string
	items            []Item
	SubtotalAmount   shared.Money
	TaxAmount        shared.Money
	TotalAmount      shared.Money
	CreatedAt        time.Time
}

// NewCreditNote creates a credit note with validation.
// CreditNoteNumber is left as 0; it is assigned by the repository on save.
func NewCreditNote(id, invoiceID, orderID, customerID, reason, currency string, items []Item, taxAmount shared.Money) (CreditNote, error) {
	if id == "" {
		return CreditNote{}, errors.New("credit note: id must not be empty")
	}
	if invoiceID == "" {
		return CreditNote{}, errors.New("credit note: invoice id must not be empty")
	}
	if orderID == "" {
		return CreditNote{}, errors.New("credit note: order id must not be empty")
	}
	if customerID == "" {
		return CreditNote{}, errors.New("credit note: customer id must not be empty")
	}
	if reason == "" {
		return CreditNote{}, errors.New("credit note: reason must not be empty")
	}
	if !shared.IsValidCurrency(currency) {
		return CreditNote{}, errors.New("credit note: invalid currency code")
	}
	if len(items) == 0 {
		return CreditNote{}, errors.New("credit note: must contain at least one item")
	}
	if taxAmount.Currency() != currency {
		return CreditNote{}, errors.New("credit note: tax amount currency mismatch")
	}
	if taxAmount.IsNegative() {
		return CreditNote{}, errors.New("credit note: tax amount must be non-negative")
	}
	for i := range items {
		if items[i].UnitPrice.Currency() != currency {
			return CreditNote{}, errors.New("credit note: item currency mismatch")
		}
	}

	subtotal, err := computeSubtotal(items, currency)
	if err != nil {
		return CreditNote{}, err
	}

	total, err := subtotal.AddChecked(taxAmount)
	if err != nil {
		return CreditNote{}, errors.New("credit note: total overflow")
	}

	cp := make([]Item, len(items))
	copy(cp, items)

	return CreditNote{
		ID:             id,
		InvoiceID:      invoiceID,
		OrderID:        orderID,
		CustomerID:     customerID,
		Reason:         reason,
		Currency:       currency,
		items:          cp,
		SubtotalAmount: subtotal,
		TaxAmount:      taxAmount,
		TotalAmount:    total,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

// Items returns a defensive copy of the credit note's line items.
func (cn CreditNote) Items() []Item {
	cp := make([]Item, len(cn.items))
	copy(cp, cn.items)
	return cp
}

// SetItemsFromDB sets the items when loading from persistence.
func (cn *CreditNote) SetItemsFromDB(items []Item) error {
	subtotal, err := computeSubtotal(items, cn.Currency)
	if err != nil {
		return err
	}
	if !subtotal.Equal(cn.SubtotalAmount) {
		return errors.New("credit note: items subtotal does not match header")
	}
	cp := make([]Item, len(items))
	copy(cp, items)
	cn.items = cp
	return nil
}
