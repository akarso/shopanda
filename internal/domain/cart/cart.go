package cart

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// CartStatus represents the lifecycle state of a cart.
type CartStatus string

const (
	CartStatusActive     CartStatus = "active"
	CartStatusCheckedOut CartStatus = "checked_out"
	CartStatusAbandoned  CartStatus = "abandoned"
)

// IsValid returns true if s is a recognised cart status.
func (s CartStatus) IsValid() bool {
	switch s {
	case CartStatusActive, CartStatusCheckedOut, CartStatusAbandoned:
		return true
	}
	return false
}

// Cart represents a shopping cart.
type Cart struct {
	ID         string
	CustomerID string // empty for anonymous/guest carts
	status     CartStatus
	Currency   string
	CouponCode string // applied coupon code, empty = none
	Items      []Item
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewCart creates a Cart with validation. customerID may be empty for guest carts.
func NewCart(id, currency string) (Cart, error) {
	if id == "" {
		return Cart{}, errors.New("cart: id must not be empty")
	}
	if !shared.IsValidCurrency(currency) {
		return Cart{}, errors.New("cart: invalid currency code")
	}
	now := time.Now().UTC()
	return Cart{
		ID:        id,
		status:    CartStatusActive,
		Currency:  currency,
		Items:     nil,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// SetCustomerID assigns the cart to a customer.
func (c *Cart) SetCustomerID(customerID string) error {
	if customerID == "" {
		return errors.New("cart: customer id must not be empty")
	}
	c.CustomerID = customerID
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Status returns the current cart status.
func (c Cart) Status() CartStatus {
	return c.status
}

// IsActive returns true if the cart is in active status.
func (c Cart) IsActive() bool {
	return c.status == CartStatusActive
}

// Checkout transitions the cart from active to checked_out.
func (c *Cart) Checkout() error {
	if c.status != CartStatusActive {
		return errors.New("cart: only active carts can be checked out")
	}
	c.status = CartStatusCheckedOut
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Abandon transitions the cart from active to abandoned.
func (c *Cart) Abandon() error {
	if c.status != CartStatusActive {
		return errors.New("cart: only active carts can be abandoned")
	}
	c.status = CartStatusAbandoned
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// SetStatusFromDB sets the cart status during reconstruction from a data store.
// The provided status must be a valid CartStatus value.
func (c *Cart) SetStatusFromDB(s CartStatus) error {
	if !s.IsValid() {
		return errors.New("cart: invalid status from database")
	}
	c.status = s
	return nil
}

// AddItem adds an item to the cart. If an item with the same VariantID already
// exists, the quantities are summed and UnitPrice is overwritten with the
// latest value provided.
func (c *Cart) AddItem(variantID string, quantity int, unitPrice shared.Money) error {
	if !c.IsActive() {
		return errors.New("cart: cannot modify non-active cart")
	}
	if variantID == "" {
		return errors.New("cart: variant id must not be empty")
	}
	if quantity <= 0 {
		return errors.New("cart: quantity must be positive")
	}
	if unitPrice.Currency() != c.Currency {
		return errors.New("cart: item currency must match cart currency")
	}

	for i := range c.Items {
		if c.Items[i].VariantID == variantID {
			c.Items[i].Quantity += quantity
			c.Items[i].UnitPrice = unitPrice
			c.Items[i].UpdatedAt = time.Now().UTC()
			c.UpdatedAt = time.Now().UTC()
			return nil
		}
	}

	item, err := NewItem(variantID, quantity, unitPrice)
	if err != nil {
		return err
	}
	c.Items = append(c.Items, item)
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// UpdateItemQuantity sets the quantity of an existing item.
func (c *Cart) UpdateItemQuantity(variantID string, quantity int) error {
	if !c.IsActive() {
		return errors.New("cart: cannot modify non-active cart")
	}
	if quantity <= 0 {
		return errors.New("cart: quantity must be positive")
	}
	for i := range c.Items {
		if c.Items[i].VariantID == variantID {
			c.Items[i].Quantity = quantity
			c.Items[i].UpdatedAt = time.Now().UTC()
			c.UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return errors.New("cart: item not found")
}

// RemoveItem removes an item by variant ID.
func (c *Cart) RemoveItem(variantID string) error {
	if !c.IsActive() {
		return errors.New("cart: cannot modify non-active cart")
	}
	for i := range c.Items {
		if c.Items[i].VariantID == variantID {
			c.Items = append(c.Items[:i], c.Items[i+1:]...)
			c.UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return errors.New("cart: item not found")
}

// ItemCount returns the total number of distinct items.
func (c Cart) ItemCount() int {
	return len(c.Items)
}

// TotalQuantity returns the sum of quantities across all items.
func (c Cart) TotalQuantity() int {
	total := 0
	for _, item := range c.Items {
		total += item.Quantity
	}
	return total
}

// ApplyCoupon sets the coupon code on an active cart.
func (c *Cart) ApplyCoupon(code string) error {
	if !c.IsActive() {
		return errors.New("cart: cannot modify non-active cart")
	}
	if code == "" {
		return errors.New("cart: coupon code must not be empty")
	}
	c.CouponCode = code
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// RemoveCoupon clears the coupon code from an active cart.
func (c *Cart) RemoveCoupon() error {
	if !c.IsActive() {
		return errors.New("cart: cannot modify non-active cart")
	}
	c.CouponCode = ""
	c.UpdatedAt = time.Now().UTC()
	return nil
}
