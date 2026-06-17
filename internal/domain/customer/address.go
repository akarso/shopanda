package customer

import (
	"errors"
	"strings"
	"time"
)

// Address is a reusable shipping address saved to a customer's account.
//
// It is distinct from the immutable address snapshot stored on an order: saved
// addresses are mutable account data used to prefill checkout, while order
// addresses are frozen at purchase time.
type Address struct {
	ID         string
	CustomerID string
	Label      string
	Recipient  string
	Street     string
	City       string
	Postcode   string
	Country    string
	IsDefault  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewAddress creates a saved address after validating the required fields.
// ID and CustomerID must be supplied by the caller (e.g. id.New()).
func NewAddress(id, customerID, label, recipient, street, city, postcode, country string) (Address, error) {
	a := Address{
		ID:         strings.TrimSpace(id),
		CustomerID: strings.TrimSpace(customerID),
		Label:      strings.TrimSpace(label),
		Recipient:  strings.TrimSpace(recipient),
		Street:     strings.TrimSpace(street),
		City:       strings.TrimSpace(city),
		Postcode:   strings.TrimSpace(postcode),
		Country:    strings.TrimSpace(country),
	}
	if a.ID == "" {
		return Address{}, errors.New("address: id must not be empty")
	}
	if a.CustomerID == "" {
		return Address{}, errors.New("address: customer_id must not be empty")
	}
	if err := a.Validate(); err != nil {
		return Address{}, err
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	return a, nil
}

// Validate ensures the address carries the minimum fields a shipment needs.
func (a *Address) Validate() error {
	switch {
	case strings.TrimSpace(a.Recipient) == "":
		return errors.New("recipient is required")
	case strings.TrimSpace(a.Street) == "":
		return errors.New("street is required")
	case strings.TrimSpace(a.City) == "":
		return errors.New("city is required")
	case strings.TrimSpace(a.Postcode) == "":
		return errors.New("postcode is required")
	case strings.TrimSpace(a.Country) == "":
		return errors.New("country is required")
	default:
		return nil
	}
}

// Apply normalizes and replaces the editable fields of an address.
func (a *Address) Apply(label, recipient, street, city, postcode, country string) error {
	updated := Address{
		Label:     strings.TrimSpace(label),
		Recipient: strings.TrimSpace(recipient),
		Street:    strings.TrimSpace(street),
		City:      strings.TrimSpace(city),
		Postcode:  strings.TrimSpace(postcode),
		Country:   strings.TrimSpace(country),
	}
	if err := updated.Validate(); err != nil {
		return err
	}
	a.Label = updated.Label
	a.Recipient = updated.Recipient
	a.Street = updated.Street
	a.City = updated.City
	a.Postcode = updated.Postcode
	a.Country = updated.Country
	a.UpdatedAt = time.Now().UTC()
	return nil
}
