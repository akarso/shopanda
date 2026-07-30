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
	evaluators *promotion.EvaluatorRegistry
}

// NewCatalogPromotionStep returns a new CatalogPromotionStep.
func NewCatalogPromotionStep(
	promotions promotion.PromotionRepository,
	coupons promotion.CouponRepository,
	evaluators *promotion.EvaluatorRegistry,
) *CatalogPromotionStep {
	return &CatalogPromotionStep{
		promotions: promotions,
		coupons:    coupons,
		evaluators: evaluators,
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

		cond, err := decodeCatalogCondition(p.Conditions, s.evaluators)
		if err != nil {
			return fmt.Errorf("catalog promotions: %q: conditions: %w", p.Name, err)
		}
		act, err := decodeCatalogAction(p.Actions, s.evaluators)
		if err != nil {
			return fmt.Errorf("catalog promotions: %q: actions: %w", p.Name, err)
		}

		for i := range pctx.Items {
			item := &pctx.Items[i]
			ok, err := cond.matches(ctx, item, s.evaluators)
			if err != nil {
				return fmt.Errorf("catalog promotions: %q: condition: %w", p.Name, err)
			}
			if !ok {
				continue
			}
			discount, err := act.compute(ctx, item, pctx.Currency, s.evaluators)
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
	typ       string
	value     int
	raw       []byte
	usePlugin bool
}

func decodeCatalogCondition(data []byte, reg *promotion.EvaluatorRegistry) (catalogCondition, error) {
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
		if reg != nil && reg.HasCatalogCondition(cfg.Type) {
			return catalogCondition{typ: cfg.Type, raw: append([]byte(nil), data...), usePlugin: true}, nil
		}
		return catalogCondition{}, fmt.Errorf("unknown condition type: %q", cfg.Type)
	}
}

func (c catalogCondition) matches(ctx context.Context, item *domain.PricingItem, reg *promotion.EvaluatorRegistry) (bool, error) {
	if c.usePlugin {
		return reg.EvalCatalogCondition(ctx, c.typ, c.raw, item)
	}
	switch c.typ {
	case "always":
		return true, nil
	case "min_quantity":
		return item.Quantity >= c.value, nil
	default:
		return false, nil
	}
}

type actionConfig struct {
	Type       string `json:"type"`
	Percentage int    `json:"percentage,omitempty"`
	Amount     int64  `json:"amount,omitempty"`
	Tiers      []struct {
		MinQty     int `json:"min_qty"`
		Percentage int `json:"percentage"`
	} `json:"tiers,omitempty"`
	BuyQty int `json:"buy_qty,omitempty"`
	GetQty int `json:"get_qty,omitempty"`
}

type promotionTier struct {
	minQty     int
	percentage int
}

type catalogAction struct {
	typ        string
	percentage int
	amount     int64
	tiers      []promotionTier
	buyQty     int
	getQty     int
	raw        []byte
	usePlugin  bool
}

func decodeCatalogAction(data []byte, reg *promotion.EvaluatorRegistry) (catalogAction, error) {
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
	case "tiered":
		if len(cfg.Tiers) == 0 {
			return catalogAction{}, fmt.Errorf("tiered action requires at least one tier")
		}
		tiers := make([]promotionTier, len(cfg.Tiers))
		prev := 0
		for i, tier := range cfg.Tiers {
			if tier.MinQty <= 0 {
				return catalogAction{}, fmt.Errorf("tier %d min_qty must be positive", i)
			}
			if tier.MinQty <= prev {
				return catalogAction{}, fmt.Errorf("tier min_qty values must be strictly increasing")
			}
			if tier.Percentage <= 0 || tier.Percentage > 100 {
				return catalogAction{}, fmt.Errorf("tier %d percentage must be 1-100", i)
			}
			tiers[i] = promotionTier{minQty: tier.MinQty, percentage: tier.Percentage}
			prev = tier.MinQty
		}
		return catalogAction{typ: cfg.Type, tiers: tiers}, nil
	case "buy_x_get_y":
		if cfg.BuyQty <= 0 {
			return catalogAction{}, fmt.Errorf("buy_qty must be positive")
		}
		if cfg.GetQty <= 0 {
			return catalogAction{}, fmt.Errorf("get_qty must be positive")
		}
		return catalogAction{typ: cfg.Type, buyQty: cfg.BuyQty, getQty: cfg.GetQty}, nil
	default:
		if reg != nil && reg.HasCatalogAction(cfg.Type) {
			return catalogAction{typ: cfg.Type, raw: append([]byte(nil), data...), usePlugin: true}, nil
		}
		return catalogAction{}, fmt.Errorf("unknown action type: %q", cfg.Type)
	}
}

func (a catalogAction) compute(ctx context.Context, item *domain.PricingItem, currency string, reg *promotion.EvaluatorRegistry) (shared.Money, error) {
	if a.usePlugin {
		return reg.EvalCatalogAction(ctx, a.typ, a.raw, item, currency)
	}
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
	case "tiered":
		pct := a.tierPercentage(item.Quantity)
		if pct == 0 {
			return shared.Zero(currency)
		}
		raw := item.Total.Amount() * int64(pct) / 100
		return shared.NewMoney(raw, currency)
	case "buy_x_get_y":
		bundle := a.buyQty + a.getQty
		sets := item.Quantity / bundle
		if sets == 0 {
			return shared.Zero(currency)
		}
		freeItems := sets * a.getQty
		raw := item.UnitPrice.Amount() * int64(freeItems)
		if raw > item.Total.Amount() {
			raw = item.Total.Amount()
		}
		return shared.NewMoney(raw, currency)
	default:
		return shared.Money{}, fmt.Errorf("unsupported action: %q", a.typ)
	}
}

func (a catalogAction) tierPercentage(qty int) int {
	matched := 0
	for _, tier := range a.tiers {
		if qty >= tier.minQty {
			matched = tier.percentage
		}
	}
	return matched
}
