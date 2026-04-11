package promotion

import (
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/platform/id"
)

// PromotionType classifies the scope of a promotion.
type PromotionType string

const (
	// TypeCatalog applies per-item discounts during pricing.
	TypeCatalog PromotionType = "catalog"
	// TypeCart applies order-level discounts to the cart total.
	TypeCart PromotionType = "cart"
)

// IsValid returns true if t is a recognised promotion type.
func (t PromotionType) IsValid() bool {
	switch t {
	case TypeCatalog, TypeCart:
		return true
	}
	return false
}

// Promotion defines a discount rule that can be applied to catalog items or
// cart totals. Conditions and Actions are stored as opaque JSON to keep the
// domain entity storage-friendly while letting the application layer
// interpret them via the rule system.
type Promotion struct {
	ID          string
	Name        string
	Type        PromotionType
	Priority    int // lower = higher priority
	Active      bool
	StartAt     *time.Time // nil means immediately active
	EndAt       *time.Time // nil means no expiry
	Conditions  []byte     // JSON-encoded condition config
	Actions     []byte     // JSON-encoded action config
	CouponBound bool       // true if activation requires a coupon code
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewPromotion creates a Promotion with the required fields.
func NewPromotion(promoID, name string, typ PromotionType) (Promotion, error) {
	if promoID == "" {
		return Promotion{}, errors.New("promotion: id must not be empty")
	}
	if !id.IsValid(promoID) {
		return Promotion{}, errors.New("promotion: id must be a valid UUID")
	}
	if name == "" {
		return Promotion{}, errors.New("promotion: name must not be empty")
	}
	if !typ.IsValid() {
		return Promotion{}, fmt.Errorf("promotion: invalid type: %q", typ)
	}
	now := time.Now().UTC()
	return Promotion{
		ID:        promoID,
		Name:      name,
		Type:      typ,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// IsEligible returns true if the promotion is active and the current time
// falls within the optional start/end window.
func (p Promotion) IsEligible(now time.Time) bool {
	if !p.Active {
		return false
	}
	if p.StartAt != nil && now.Before(*p.StartAt) {
		return false
	}
	if p.EndAt != nil && now.After(*p.EndAt) {
		return false
	}
	return true
}
