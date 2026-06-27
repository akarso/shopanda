package order

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// OrderStatus represents the lifecycle state of an order.
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusConfirmed OrderStatus = "confirmed"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusFailed    OrderStatus = "failed"
)

// IsValid returns true if s is a recognised order status.
func (s OrderStatus) IsValid() bool {
	switch s {
	case OrderStatusPending, OrderStatusConfirmed, OrderStatusPaid,
		OrderStatusCancelled, OrderStatusFailed:
		return true
	}
	return false
}

// Order represents a finalised purchase snapshot.
type Order struct {
	ID           string
	CustomerID   string
	ContactEmail string
	status       OrderStatus
	Currency     string
	items        []Item
	TotalAmount  shared.Money
	// DestinationCountry is the ISO 3166-1 alpha-2 shipping country at checkout.
	DestinationCountry string
	// TaxAmount is the VAT total applied at checkout (zero when unset).
	TaxAmount    shared.Money
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewOrder creates an Order in pending status with validation.
func NewOrder(id, customerID, contactEmail, currency string, items []Item) (Order, error) {
	if id == "" {
		return Order{}, errors.New("order: id must not be empty")
	}
	customerID = strings.TrimSpace(customerID)
	contactEmail = strings.ToLower(strings.TrimSpace(contactEmail))
	if customerID == "" {
		if contactEmail == "" {
			return Order{}, errors.New("order: contact email must not be empty for guest orders")
		}
		if _, err := mail.ParseAddress(contactEmail); err != nil {
			return Order{}, errors.New("order: invalid contact email")
		}
	} else if contactEmail != "" {
		if _, err := mail.ParseAddress(contactEmail); err != nil {
			return Order{}, errors.New("order: invalid contact email")
		}
	}
	if !shared.IsValidCurrency(currency) {
		return Order{}, errors.New("order: invalid currency code")
	}
	if len(items) == 0 {
		return Order{}, errors.New("order: must contain at least one item")
	}
	seen := make(map[string]struct{}, len(items))
	for i := range items {
		if _, dup := seen[items[i].VariantID]; dup {
			return Order{}, errors.New("order: duplicate variant id")
		}
		seen[items[i].VariantID] = struct{}{}
	}
	for i := range items {
		if items[i].UnitPrice.Currency() != currency {
			return Order{}, errors.New("order: item currency mismatch")
		}
	}

	total, err := computeTotal(items, currency)
	if err != nil {
		return Order{}, err
	}

	// Defensive copy of items slice.
	cp := make([]Item, len(items))
	copy(cp, items)

	taxZero, err := shared.Zero(currency)
	if err != nil {
		return Order{}, err
	}

	now := time.Now().UTC()
	return Order{
		ID:           id,
		CustomerID:   customerID,
		ContactEmail: contactEmail,
		status:       OrderStatusPending,
		Currency:     currency,
		items:        cp,
		TotalAmount:  total,
		TaxAmount:    taxZero,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// SetTaxSnapshot records the shipping destination and VAT total for OSS reporting.
func (o *Order) SetTaxSnapshot(destinationCountry string, taxAmount shared.Money) error {
	normalized, err := ValidateDestinationCountry(destinationCountry)
	if err != nil {
		return err
	}
	if taxAmount.Currency() != o.Currency {
		return errors.New("order: tax amount currency mismatch")
	}
	if taxAmount.IsNegative() {
		return errors.New("order: tax amount must be non-negative")
	}
	o.DestinationCountry = normalized
	o.TaxAmount = taxAmount
	return nil
}

// ValidateDestinationCountry normalizes and validates an ISO 3166-1 alpha-2 code.
func ValidateDestinationCountry(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "", errors.New("order: destination country must not be empty")
	}
	if len(code) != 2 {
		return "", errors.New("order: destination country must be a 2-letter ISO code")
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return "", errors.New("order: destination country must be a 2-letter ISO code")
		}
	}
	return code, nil
}

// Items returns a defensive copy of the order's line items.
func (o Order) Items() []Item {
	cp := make([]Item, len(o.items))
	copy(cp, o.items)
	return cp
}

// Status returns the current order status.
func (o Order) Status() OrderStatus {
	return o.status
}

// Confirm transitions the order from pending to confirmed.
func (o *Order) Confirm() error {
	if o.status != OrderStatusPending {
		return errors.New("order: can only confirm a pending order")
	}
	o.status = OrderStatusConfirmed
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkPaid transitions the order from confirmed to paid.
func (o *Order) MarkPaid() error {
	if o.status != OrderStatusConfirmed {
		return errors.New("order: can only mark a confirmed order as paid")
	}
	o.status = OrderStatusPaid
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// Cancel transitions the order to cancelled.
// Only pending or confirmed orders may be cancelled.
func (o *Order) Cancel() error {
	if o.status != OrderStatusPending && o.status != OrderStatusConfirmed {
		return errors.New("order: can only cancel a pending or confirmed order")
	}
	o.status = OrderStatusCancelled
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// Fail transitions the order from pending to failed.
func (o *Order) Fail() error {
	if o.status != OrderStatusPending {
		return errors.New("order: can only fail a pending order")
	}
	o.status = OrderStatusFailed
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// SetStatusFromDB restores the status when loading from persistence.
func (o *Order) SetStatusFromDB(s string) error {
	status := OrderStatus(s)
	if !status.IsValid() {
		return errors.New("order: invalid status from db: " + s)
	}
	o.status = status
	return nil
}

// SetItemsFromDB sets the items when loading from persistence.
// Returns an error if the items total doesn't match the order header.
func (o *Order) SetItemsFromDB(items []Item) error {
	total, err := computeTotal(items, o.Currency)
	if err != nil {
		return err
	}
	if !total.Equal(o.TotalAmount) {
		return errors.New("order: items total does not match order header")
	}
	cp := make([]Item, len(items))
	copy(cp, items)
	o.items = cp
	return nil
}

// LinkToCustomer associates a guest order with a customer after registration.
// Used in account-linking workflow to convert guest orders to authenticated.
func (o *Order) LinkToCustomer(customerID string) error {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return errors.New("order: customer id must not be empty")
	}
	if o.CustomerID != "" {
		return errors.New("order: already linked to a customer")
	}
	o.CustomerID = customerID
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// computeTotal sums item line totals with overflow checking.
func computeTotal(items []Item, currency string) (shared.Money, error) {
	total, err := shared.Zero(currency)
	if err != nil {
		return shared.Money{}, err
	}
	for i := range items {
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
