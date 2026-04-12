package invoice

import (
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// InvoiceStatus represents the state of an invoice.
type InvoiceStatus string

const (
	InvoiceStatusIssued InvoiceStatus = "issued"
)

// IsValid returns true if s is a recognised invoice status.
func (s InvoiceStatus) IsValid() bool {
	switch s {
	case InvoiceStatusIssued:
		return true
	}
	return false
}

// Invoice is an immutable financial document that snapshots an order.
type Invoice struct {
	id             string
	invoiceNumber  int64 // assigned by DB sequence on save
	orderID        string
	customerID     string
	status         InvoiceStatus
	currency       string
	items          []Item
	subtotalAmount shared.Money
	taxAmount      shared.Money
	totalAmount    shared.Money
	createdAt      time.Time
}

// NewInvoice creates an issued invoice with validation.
// InvoiceNumber is left as 0; it is assigned by the repository on save.
func NewInvoice(id, orderID, customerID, currency string, items []Item, taxAmount shared.Money) (Invoice, error) {
	if id == "" {
		return Invoice{}, errors.New("invoice: id must not be empty")
	}
	if orderID == "" {
		return Invoice{}, errors.New("invoice: order id must not be empty")
	}
	if customerID == "" {
		return Invoice{}, errors.New("invoice: customer id must not be empty")
	}
	if !shared.IsValidCurrency(currency) {
		return Invoice{}, errors.New("invoice: invalid currency code")
	}
	if len(items) == 0 {
		return Invoice{}, errors.New("invoice: must contain at least one item")
	}
	if taxAmount.Currency() != currency {
		return Invoice{}, errors.New("invoice: tax amount currency mismatch")
	}
	if taxAmount.IsNegative() {
		return Invoice{}, errors.New("invoice: tax amount must be non-negative")
	}
	for i := range items {
		if err := items[i].Validate(currency); err != nil {
			return Invoice{}, fmt.Errorf("invoice: invalid item: %w", err)
		}
	}

	subtotal, err := computeSubtotal(items, currency)
	if err != nil {
		return Invoice{}, err
	}

	total, err := subtotal.AddChecked(taxAmount)
	if err != nil {
		return Invoice{}, errors.New("invoice: total overflow")
	}

	cp := make([]Item, len(items))
	copy(cp, items)

	return Invoice{
		id:             id,
		orderID:        orderID,
		customerID:     customerID,
		status:         InvoiceStatusIssued,
		currency:       currency,
		items:          cp,
		subtotalAmount: subtotal,
		taxAmount:      taxAmount,
		totalAmount:    total,
		createdAt:      time.Now().UTC(),
	}, nil
}

// Items returns a defensive copy of the invoice's line items.
func (inv Invoice) Items() []Item {
	cp := make([]Item, len(inv.items))
	copy(cp, inv.items)
	return cp
}

// ID returns the invoice identifier.
func (inv Invoice) ID() string { return inv.id }

// InvoiceNumber returns the sequential number assigned by the DB.
func (inv Invoice) InvoiceNumber() int64 { return inv.invoiceNumber }

// OrderID returns the associated order identifier.
func (inv Invoice) OrderID() string { return inv.orderID }

// CustomerID returns the associated customer identifier.
func (inv Invoice) CustomerID() string { return inv.customerID }

// Status returns the current invoice status.
func (inv Invoice) Status() InvoiceStatus {
	return inv.status
}

// Currency returns the invoice currency code.
func (inv Invoice) Currency() string { return inv.currency }

// SubtotalAmount returns the invoice subtotal (sum of line totals).
func (inv Invoice) SubtotalAmount() shared.Money { return inv.subtotalAmount }

// TaxAmount returns the invoice tax amount.
func (inv Invoice) TaxAmount() shared.Money { return inv.taxAmount }

// TotalAmount returns the invoice grand total (subtotal + tax).
func (inv Invoice) TotalAmount() shared.Money { return inv.totalAmount }

// CreatedAt returns the invoice creation timestamp.
func (inv Invoice) CreatedAt() time.Time { return inv.createdAt }

// NewInvoiceFromDB reconstructs an Invoice from persisted data.
// Status must be set separately via SetStatusFromDB, and items via SetItemsFromDB.
func NewInvoiceFromDB(id string, invoiceNumber int64, orderID, customerID, currency string,
	subtotalAmount, taxAmount, totalAmount shared.Money, createdAt time.Time) Invoice {
	return Invoice{
		id:             id,
		invoiceNumber:  invoiceNumber,
		orderID:        orderID,
		customerID:     customerID,
		currency:       currency,
		subtotalAmount: subtotalAmount,
		taxAmount:      taxAmount,
		totalAmount:    totalAmount,
		createdAt:      createdAt,
	}
}

// SetInvoiceNumberFromDB assigns the DB-generated invoice number after save.
func (inv *Invoice) SetInvoiceNumberFromDB(n int64) {
	inv.invoiceNumber = n
}

// SetStatusFromDB restores the status when loading from persistence.
func (inv *Invoice) SetStatusFromDB(s string) error {
	status := InvoiceStatus(s)
	if !status.IsValid() {
		return errors.New("invoice: invalid status from db: " + s)
	}
	inv.status = status
	return nil
}

// SetItemsFromDB sets the items when loading from persistence.
// Returns an error if items are empty, if any item is malformed, if the items
// subtotal doesn't match, or if the header amounts are inconsistent.
func (inv *Invoice) SetItemsFromDB(items []Item) error {
	if len(items) == 0 {
		return errors.New("invoice: items must not be empty")
	}
	for i := range items {
		if err := items[i].Validate(inv.currency); err != nil {
			return fmt.Errorf("invoice: invalid item: %w", err)
		}
	}
	subtotal, err := computeSubtotal(items, inv.currency)
	if err != nil {
		return err
	}
	if !subtotal.Equal(inv.subtotalAmount) {
		return errors.New("invoice: items subtotal does not match invoice header")
	}
	expectedTotal, err := subtotal.AddChecked(inv.taxAmount)
	if err != nil {
		return errors.New("invoice: header total overflow during validation")
	}
	if !expectedTotal.Equal(inv.totalAmount) {
		return errors.New("invoice: subtotal + tax does not equal total")
	}
	cp := make([]Item, len(items))
	copy(cp, items)
	inv.items = cp
	return nil
}

// computeSubtotal sums item line totals with overflow checking.
func computeSubtotal(items []Item, currency string) (shared.Money, error) {
	total, err := shared.Zero(currency)
	if err != nil {
		return shared.Money{}, err
	}
	for i := range items {
		if items[i].UnitPrice.Currency() != currency {
			return shared.Money{}, fmt.Errorf("invoice: item %q currency %s does not match %s",
				items[i].VariantID, items[i].UnitPrice.Currency(), currency)
		}
		lt, err := items[i].LineTotal()
		if err != nil {
			return shared.Money{}, err
		}
		total, err = total.AddChecked(lt)
		if err != nil {
			return shared.Money{}, err
		}
	}
	return total, nil
}
