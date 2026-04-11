package promotion

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/akarso/shopanda/internal/platform/id"
)

// couponCodeRegex matches alphanumeric coupon codes (uppercase, digits, hyphens).
var couponCodeRegex = regexp.MustCompile(`^[A-Z0-9][A-Z0-9\-]{1,48}[A-Z0-9]$`)

// Coupon is a redeemable code that activates a coupon-bound promotion.
type Coupon struct {
	ID          string
	Code        string
	PromotionID string
	UsageLimit  int // 0 = unlimited
	UsageCount  int
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewCoupon creates a Coupon with the required fields.
func NewCoupon(couponID, code, promotionID string) (Coupon, error) {
	if couponID == "" {
		return Coupon{}, errors.New("coupon: id must not be empty")
	}
	if !id.IsValid(couponID) {
		return Coupon{}, errors.New("coupon: id must be a valid UUID")
	}
	if !couponCodeRegex.MatchString(code) {
		return Coupon{}, fmt.Errorf("coupon: invalid code format: %q", code)
	}
	if promotionID == "" {
		return Coupon{}, errors.New("coupon: promotion id must not be empty")
	}
	if !id.IsValid(promotionID) {
		return Coupon{}, errors.New("coupon: promotion id must be a valid UUID")
	}
	now := time.Now().UTC()
	return Coupon{
		ID:          couponID,
		Code:        code,
		PromotionID: promotionID,
		Active:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// CanRedeem returns true if the coupon is active and has not exceeded its
// usage limit (0 = unlimited).
func (c Coupon) CanRedeem() bool {
	if !c.Active {
		return false
	}
	if c.UsageLimit > 0 && c.UsageCount >= c.UsageLimit {
		return false
	}
	return true
}

// Redeem increments the usage count. Returns an error if the coupon cannot
// be redeemed.
func (c *Coupon) Redeem() error {
	if !c.CanRedeem() {
		return fmt.Errorf("coupon %q: cannot redeem", c.Code)
	}
	c.UsageCount++
	c.UpdatedAt = time.Now().UTC()
	return nil
}
