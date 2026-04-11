package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// CatalogPromotionStep applies catalog-level promotions as per-item discount
// adjustments in the pricing pipeline.
//
// Automatic promotions (CouponBound=false) always apply when eligible.
// Coupon-bound promotions apply only when Meta["coupon_code"] matches a valid
// coupon linked to the promotion.
type CatalogPromotionStep struct {
	promotions promotion.PromotionRepository
	coupons    promotion.CouponRepository
}

// NewCatalogPromotionStep returns a new CatalogPromotionStep.
func NewCatalogPromotionStep(
	promotions promotion.PromotionRepository,
	coupons promotion.CouponRepository,
) *CatalogPromotionStep {
	return &CatalogPromotionStep{
		promotions: promotions,
		coupons:    coupons,
	}
}

func (s *CatalogPromotionStep) Name() string { return "catalog_promotions" }

// Apply loads active catalog promotions and applies matching discounts to
// each item in the pricing context.
func (s *CatalogPromotionStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
	promos, err := s.promotions.ListActive(ctx, promotion.TypeCatalog)
	if err != nil {
		return fmt.Errorf("catalog promotions: list: %w", err)
	}
	if len(promos) == 0 {
		return nil
	}

	now := time.Now()
	couponCode, _ := pctx.Meta["coupon_code"].(string)

	for _, p := range promos {
		if !p.IsEligible(now) {
			continue
		}
		if p.CouponBound {
			if couponCode == "" {
				continue
			}
			coupon, err := s.coupons.FindByCode(ctx, couponCode)
			if err != nil {
				return fmt.Errorf("catalog promotions: coupon lookup: %w", err)
			}
			if coupon == nil || coupon.PromotionID != p.ID || !coupon.CanRedeem() {
				continue
			}
		}

		cond, err := decodeCatalogCondition(p.Conditions)
		if err != nil {
			return fmt.Errorf("catalog promotions: %q: conditions: %w", p.Name, err)
		}
		act, err := decodeCatalogAction(p.Actions)
		if err != nil {
			return fmt.Errorf("catalog promotions: %q: actions: %w", p.Name, err)
		}

		for i := range pctx.Items {
			item := &pctx.Items[i]
			if !cond.matches(item) {
				continue
			}
			discount, err := act.compute(item, pctx.Currency)
			if err != nil {
				return fmt.Errorf("catalog promotions: %q: compute: %w", p.Name, err)
			}
			if discount.IsZero() {
				continue
			}
			adj, err := domain.NewAdjustment(domain.AdjustmentDiscount, "promo."+p.ID, discount)
			if err != nil {
				return fmt.Errorf("catalog promotions: %q: adjustment: %w", p.Name, err)
			}
			adj.Description = p.Name
			item.Adjustments = append(item.Adjustments, adj)
		}
	}
	return nil
}

// ── condition / action decoding ─────────────────────────────────────────

type conditionConfig struct {
	Type  string `json:"type"`
	Value int    `json:"value,omitempty"`
}

type catalogCondition struct {
	typ   string
	value int
}

func decodeCatalogCondition(data []byte) (catalogCondition, error) {
	if len(data) == 0 || string(data) == "null" {
		return catalogCondition{typ: "always"}, nil
	}
	var cfg conditionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return catalogCondition{}, fmt.Errorf("decode: %w", err)
	}
	switch cfg.Type {
	case "always":
		return catalogCondition{typ: cfg.Type}, nil
	case "min_quantity":
		if cfg.Value <= 0 {
			return catalogCondition{}, fmt.Errorf("min_quantity value must be positive, got %d", cfg.Value)
		}
		return catalogCondition{typ: cfg.Type, value: cfg.Value}, nil
	default:
		return catalogCondition{}, fmt.Errorf("unknown condition type: %q", cfg.Type)
	}
}

func (c catalogCondition) matches(item *domain.PricingItem) bool {
	switch c.typ {
	case "always":
		return true
	case "min_quantity":
		return item.Quantity >= c.value
	default:
		return false
	}
}

type actionConfig struct {
	Type       string `json:"type"`
	Percentage int    `json:"percentage,omitempty"` // whole percentage, e.g. 10 = 10%
	Amount     int64  `json:"amount,omitempty"`     // minor currency units
}

type catalogAction struct {
	typ        string
	percentage int
	amount     int64
}

func decodeCatalogAction(data []byte) (catalogAction, error) {
	if len(data) == 0 || string(data) == "null" {
		return catalogAction{}, fmt.Errorf("action config is required")
	}
	var cfg actionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return catalogAction{}, fmt.Errorf("decode: %w", err)
	}
	switch cfg.Type {
	case "percentage":
		if cfg.Percentage <= 0 || cfg.Percentage > 100 {
			return catalogAction{}, fmt.Errorf("percentage must be 1-100, got %d", cfg.Percentage)
		}
		return catalogAction{typ: cfg.Type, percentage: cfg.Percentage}, nil
	case "fixed":
		if cfg.Amount <= 0 {
			return catalogAction{}, fmt.Errorf("fixed amount must be positive")
		}
		return catalogAction{typ: cfg.Type, amount: cfg.Amount}, nil
	default:
		return catalogAction{}, fmt.Errorf("unknown action type: %q", cfg.Type)
	}
}

func (a catalogAction) compute(item *domain.PricingItem, currency string) (shared.Money, error) {
	switch a.typ {
	case "percentage":
		raw := item.Total.Amount() * int64(a.percentage) / 100
		return shared.NewMoney(raw, currency)
	case "fixed":
		perItem, err := shared.NewMoney(a.amount, currency)
		if err != nil {
			return shared.Money{}, err
		}
		discount, err := perItem.MulChecked(int64(item.Quantity))
		if err != nil {
			return shared.Money{}, err
		}
		if discount.Amount() > item.Total.Amount() {
			return item.Total, nil
		}
		return discount, nil
	default:
		return shared.Money{}, fmt.Errorf("unsupported action: %q", a.typ)
	}
}
