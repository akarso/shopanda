package promotion_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/platform/id"
)

// --- NewCoupon ---

func TestNewCoupon_Valid(t *testing.T) {
	cid := id.New()
	pid := id.New()
	c, err := promotion.NewCoupon(cid, "SUMMER-2026", pid)
	if err != nil {
		t.Fatalf("NewCoupon: %v", err)
	}
	if c.ID != cid {
		t.Errorf("ID = %q, want %q", c.ID, cid)
	}
	if c.Code != "SUMMER-2026" {
		t.Errorf("Code = %q, want %q", c.Code, "SUMMER-2026")
	}
	if c.PromotionID != pid {
		t.Errorf("PromotionID = %q, want %q", c.PromotionID, pid)
	}
	if !c.Active {
		t.Error("expected Active=true by default")
	}
	if c.UsageLimit != 0 {
		t.Errorf("UsageLimit = %d, want 0", c.UsageLimit)
	}
	if c.UsageCount != 0 {
		t.Errorf("UsageCount = %d, want 0", c.UsageCount)
	}
}

func TestNewCoupon_EmptyID(t *testing.T) {
	_, err := promotion.NewCoupon("", "SUMMER-2026", id.New())
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewCoupon_InvalidID(t *testing.T) {
	_, err := promotion.NewCoupon("bad", "SUMMER-2026", id.New())
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestNewCoupon_InvalidCode(t *testing.T) {
	cases := []string{
		"",           // empty
		"a",          // too short
		"ab",         // too short
		"lowercase",  // not uppercase
		"GOOD CODE!", // special char
	}
	for _, code := range cases {
		_, err := promotion.NewCoupon(id.New(), code, id.New())
		if err == nil {
			t.Errorf("expected error for code %q", code)
		}
	}
}

func TestNewCoupon_ValidCodes(t *testing.T) {
	codes := []string{
		"ABC",
		"SUMMER-2026",
		"10OFF",
		"A-B",
	}
	for _, code := range codes {
		_, err := promotion.NewCoupon(id.New(), code, id.New())
		if err != nil {
			t.Errorf("NewCoupon(%q): %v", code, err)
		}
	}
}

func TestNewCoupon_EmptyPromotionID(t *testing.T) {
	_, err := promotion.NewCoupon(id.New(), "SUMMER-2026", "")
	if err == nil {
		t.Fatal("expected error for empty promotion id")
	}
}

func TestNewCoupon_InvalidPromotionID(t *testing.T) {
	_, err := promotion.NewCoupon(id.New(), "SUMMER-2026", "bad")
	if err == nil {
		t.Fatal("expected error for invalid promotion id")
	}
}

// --- CanRedeem ---

func TestCoupon_CanRedeem_Active_Unlimited(t *testing.T) {
	c, _ := promotion.NewCoupon(id.New(), "CODE10", id.New())
	if !c.CanRedeem() {
		t.Error("expected redeemable: active, unlimited")
	}
}

func TestCoupon_CanRedeem_Inactive(t *testing.T) {
	c, _ := promotion.NewCoupon(id.New(), "CODE10", id.New())
	c.Active = false
	if c.CanRedeem() {
		t.Error("expected not redeemable: inactive")
	}
}

func TestCoupon_CanRedeem_LimitReached(t *testing.T) {
	c, _ := promotion.NewCoupon(id.New(), "CODE10", id.New())
	c.UsageLimit = 5
	c.UsageCount = 5
	if c.CanRedeem() {
		t.Error("expected not redeemable: limit reached")
	}
}

func TestCoupon_CanRedeem_UnderLimit(t *testing.T) {
	c, _ := promotion.NewCoupon(id.New(), "CODE10", id.New())
	c.UsageLimit = 5
	c.UsageCount = 4
	if !c.CanRedeem() {
		t.Error("expected redeemable: under limit")
	}
}

// --- Redeem ---

func TestCoupon_Redeem(t *testing.T) {
	c, _ := promotion.NewCoupon(id.New(), "CODE10", id.New())
	if err := c.Redeem(); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if c.UsageCount != 1 {
		t.Errorf("UsageCount = %d, want 1", c.UsageCount)
	}
}

func TestCoupon_Redeem_LimitExhausted(t *testing.T) {
	c, _ := promotion.NewCoupon(id.New(), "CODE10", id.New())
	c.UsageLimit = 1
	if err := c.Redeem(); err != nil {
		t.Fatalf("first Redeem: %v", err)
	}
	if err := c.Redeem(); err == nil {
		t.Fatal("expected error on second redeem (limit exhausted)")
	}
}

func TestCoupon_Redeem_Inactive(t *testing.T) {
	c, _ := promotion.NewCoupon(id.New(), "CODE10", id.New())
	c.Active = false
	if err := c.Redeem(); err == nil {
		t.Fatal("expected error: inactive coupon")
	}
}
