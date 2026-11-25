package voucher

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// MaxCodesPerUnit caps how many codes one purchased unit may issue.
	MaxCodesPerUnit = 50
	// MaxRedemptionsCap is the hard ceiling for max_redemptions (DB CHECK mirrors).
	MaxRedemptionsCap = 1000
)

// Status values derived from redeemed_count / voided_at.
const (
	StatusIssued            = "issued"
	StatusPartiallyRedeemed = "partially_redeemed"
	StatusRedeemed          = "redeemed"
	StatusVoid              = "void"
)

// Voucher is an issued redeemable code from a paid virtual voucher line.
// It is not a coupon and not store credit — staff mark it used at the door.
type Voucher struct {
	ID             string
	Code           string
	OrderID        string
	OrderItemID    string // opaque orderID/variantID
	ProductID      string
	VariantID      string
	CustomerID     string // empty for guest until linked
	Seq            int    // 0-based index within the order line issuance
	MaxRedemptions int
	RedeemedCount  int
	VoidedAt       *time.Time
	IssuedAt       time.Time
	CreatedAt      time.Time
}

// NewVoucher builds an issued voucher with RedeemedCount 0.
func NewVoucher(
	id, code, orderID, orderItemID, productID, variantID, customerID string,
	seq, maxRedemptions int,
	issuedAt time.Time,
) (*Voucher, error) {
	now := time.Now().UTC()
	if issuedAt.IsZero() {
		issuedAt = now
	}
	v := &Voucher{
		ID:             strings.TrimSpace(id),
		Code:           NormalizeCode(code),
		OrderID:        strings.TrimSpace(orderID),
		OrderItemID:    strings.TrimSpace(orderItemID),
		ProductID:      strings.TrimSpace(productID),
		VariantID:      strings.TrimSpace(variantID),
		CustomerID:     strings.TrimSpace(customerID),
		Seq:            seq,
		MaxRedemptions: maxRedemptions,
		RedeemedCount:  0,
		IssuedAt:       issuedAt.UTC(),
		CreatedAt:      now,
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return v, nil
}

// Validate checks field-level constraints. Trims ID fields in place.
func (v *Voucher) Validate() error {
	if v == nil {
		return errors.New("voucher must not be nil")
	}
	v.ID = strings.TrimSpace(v.ID)
	v.Code = NormalizeCode(v.Code)
	v.OrderID = strings.TrimSpace(v.OrderID)
	v.OrderItemID = strings.TrimSpace(v.OrderItemID)
	v.ProductID = strings.TrimSpace(v.ProductID)
	v.VariantID = strings.TrimSpace(v.VariantID)
	v.CustomerID = strings.TrimSpace(v.CustomerID)
	if v.ID == "" {
		return errors.New("voucher: id must not be empty")
	}
	if err := ValidateCode(v.Code); err != nil {
		return err
	}
	if v.OrderID == "" {
		return errors.New("voucher: order id must not be empty")
	}
	if v.OrderItemID == "" {
		return errors.New("voucher: order item id must not be empty")
	}
	if v.ProductID == "" {
		return errors.New("voucher: product id must not be empty")
	}
	if v.VariantID == "" {
		return errors.New("voucher: variant id must not be empty")
	}
	if v.Seq < 0 {
		return errors.New("voucher: seq must not be negative")
	}
	if v.MaxRedemptions < 1 {
		return errors.New("voucher: max_redemptions must be at least 1")
	}
	if v.MaxRedemptions > MaxRedemptionsCap {
		return fmt.Errorf("voucher: max_redemptions must not exceed %d", MaxRedemptionsCap)
	}
	if v.RedeemedCount < 0 {
		return errors.New("voucher: redeemed_count must not be negative")
	}
	if v.RedeemedCount > v.MaxRedemptions {
		return errors.New("voucher: redeemed_count must not exceed max_redemptions")
	}
	if v.IssuedAt.IsZero() {
		return errors.New("voucher: issued_at must not be zero")
	}
	if v.CreatedAt.IsZero() {
		return errors.New("voucher: created_at must not be zero")
	}
	return nil
}

// Status returns the derived lifecycle status.
func (v *Voucher) Status() string {
	if v == nil {
		return StatusVoid
	}
	if v.VoidedAt != nil && !v.VoidedAt.IsZero() {
		return StatusVoid
	}
	if v.RedeemedCount >= v.MaxRedemptions {
		return StatusRedeemed
	}
	if v.RedeemedCount > 0 {
		return StatusPartiallyRedeemed
	}
	return StatusIssued
}

// CanRedeem reports whether a staff redeem is allowed.
func (v *Voucher) CanRedeem() error {
	if v == nil {
		return errors.New("voucher must not be nil")
	}
	if v.Status() == StatusVoid {
		return ErrVoid
	}
	if v.RedeemedCount >= v.MaxRedemptions {
		return ErrExhausted
	}
	return nil
}

// CanUnredeem reports whether staff may undo one redemption.
func (v *Voucher) CanUnredeem() error {
	if v == nil {
		return errors.New("voucher must not be nil")
	}
	if v.Status() == StatusVoid {
		return ErrVoid
	}
	if v.RedeemedCount < 1 {
		return ErrNothingToUnredeem
	}
	return nil
}

// OrderItemKey builds the opaque order-item id used by grants and vouchers.
func OrderItemKey(orderID, variantID string) string {
	return strings.TrimSpace(orderID) + "/" + strings.TrimSpace(variantID)
}
