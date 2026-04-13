package legal

import (
	"errors"
	"time"
)

// Consent records a customer's cookie consent preferences.
type Consent struct {
	CustomerID string
	Necessary  bool
	Analytics  bool
	Marketing  bool
	UpdatedAt  time.Time
}

// NewConsent creates a Consent with the given customer ID.
// Necessary is always true; Analytics and Marketing default to false.
func NewConsent(customerID string) (Consent, error) {
	if customerID == "" {
		return Consent{}, errors.New("consent customer_id must not be empty")
	}
	return Consent{
		CustomerID: customerID,
		Necessary:  true,
		Analytics:  false,
		Marketing:  false,
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

// Update sets the consent preferences. Necessary is always forced to true.
func (c *Consent) Update(analytics, marketing bool) {
	c.Necessary = true
	c.Analytics = analytics
	c.Marketing = marketing
	c.UpdatedAt = time.Now().UTC()
}
