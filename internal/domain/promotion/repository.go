package promotion

import "context"

// PromotionRepository defines persistence operations for promotions.
type PromotionRepository interface {
	// FindByID returns a promotion by ID. Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Promotion, error)

	// ListActive returns all active promotions of the given type, ordered by
	// priority (ascending). Promotions outside their start/end window are
	// excluded by the caller, not the query.
	ListActive(ctx context.Context, typ PromotionType) ([]Promotion, error)

	// Save creates or updates a promotion.
	Save(ctx context.Context, p *Promotion) error

	// Delete removes a promotion by ID.
	Delete(ctx context.Context, id string) error
}

// CouponRepository defines persistence operations for coupons.
type CouponRepository interface {
	// FindByCode returns a coupon by its unique code. Returns (nil, nil) when
	// not found.
	FindByCode(ctx context.Context, code string) (*Coupon, error)

	// FindByID returns a coupon by ID. Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Coupon, error)

	// ListByPromotion returns all coupons for a promotion.
	ListByPromotion(ctx context.Context, promotionID string) ([]Coupon, error)

	// Save creates or updates a coupon.
	Save(ctx context.Context, c *Coupon) error

	// Delete removes a coupon by ID.
	Delete(ctx context.Context, id string) error
}
