package voucher

import (
	"context"
	"time"
)

// ListFilter narrows register queries. Empty fields are ignored.
type ListFilter struct {
	OrderID    string
	CustomerID string
	Code       string // normalized lookup; exact match after NormalizeCode
	Status     string // derived status filter (issued/redeemed/…)
	Limit      int
	Offset     int
}

// Repository persists issued vouchers.
type Repository interface {
	Insert(ctx context.Context, v *Voucher) error
	UpdateCounts(ctx context.Context, id string, redeemedCount, expectedCount int) error
	Void(ctx context.Context, id string, at time.Time) error
	// VoidForOrderItem voids every non-void voucher on the opaque order item key.
	VoidForOrderItem(ctx context.Context, orderItemID string, at time.Time) ([]Voucher, error)
	// LinkCustomer sets customer_id on all vouchers for orderID that still have ''.
	LinkCustomer(ctx context.Context, orderID, customerID string) error
	FindByID(ctx context.Context, id string) (*Voucher, error)
	FindByCode(ctx context.Context, code string) (*Voucher, error)
	List(ctx context.Context, f ListFilter) ([]Voucher, error)
	ListByOrderID(ctx context.Context, orderID string) ([]Voucher, error)
}

// SettingsRepository persists per-product voucher flags/config.
type SettingsRepository interface {
	FindByProductID(ctx context.Context, productID string) (*Settings, error)
	Upsert(ctx context.Context, s *Settings) error
	Remove(ctx context.Context, productID string) error
}

// RedemptionRepository persists redeem/unredeem audit rows.
type RedemptionRepository interface {
	Insert(ctx context.Context, r *Redemption) error
	ListByVoucherID(ctx context.Context, voucherID string) ([]Redemption, error)
}
