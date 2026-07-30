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

// CartPromotionStep applies cart-level promotions as context-wide discount
// adjustments after catalog line discounts are computed.
type CartPromotionStep struct {
	promotions promotion.PromotionRepository
	coupons    promotion.CouponRepository
	evaluators *promotion.EvaluatorRegistry
}

// NewCartPromotionStep returns a new CartPromotionStep.
func NewCartPromotionStep(
	promotions promotion.PromotionRepository,
	coupons promotion.CouponRepository,
	evaluators *promotion.EvaluatorRegistry,
) *CartPromotionStep {
	return &CartPromotionStep{
		promotions: promotions,
		coupons:    coupons,
		evaluators: evaluators,
	}
}

func (s *CartPromotionStep) Name() string { return "cart_promotions" }

func (s *CartPromotionStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
	promos, err := s.promotions.ListActive(ctx, promotion.TypeCart)
	if err != nil {
		return fmt.Errorf("cart promotions: list: %w", err)
	}
	if len(promos) == 0 {
		return nil
	}

	subtotal := shared.MustZero(pctx.Currency)
	for _, item := range pctx.Items {
		subtotal = subtotal.Add(itemNetTotal(item))
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
				return fmt.Errorf("cart promotions: coupon lookup: %w", err)
			}
			if coupon == nil || coupon.PromotionID != p.ID || !coupon.CanRedeem() {
				continue
			}
		}

		cond, err := decodeCartCondition(p.Conditions, s.evaluators)
		if err != nil {
			return fmt.Errorf("cart promotions: %q: conditions: %w", p.Name, err)
		}
		ok, err := cond.matches(ctx, subtotal, s.evaluators)
		if err != nil {
			return fmt.Errorf("cart promotions: %q: condition: %w", p.Name, err)
		}
		if !ok {
			continue
		}

		act, err := decodeCartAction(p.Actions, s.evaluators)
		if err != nil {
			return fmt.Errorf("cart promotions: %q: actions: %w", p.Name, err)
		}
		discount, err := act.compute(ctx, subtotal, pctx.Currency, s.evaluators)
		if err != nil {
			return fmt.Errorf("cart promotions: %q: compute: %w", p.Name, err)
		}
		if discount.IsZero() {
			continue
		}

		adj, err := domain.NewAdjustment(domain.AdjustmentDiscount, "promo."+p.ID, discount)
		if err != nil {
			return fmt.Errorf("cart promotions: %q: adjustment: %w", p.Name, err)
		}
		adj.Description = p.Name
		pctx.Adjustments = append(pctx.Adjustments, adj)
	}
	return nil
}

type cartCondition struct {
	typ       string
	value     int
	raw       []byte
	usePlugin bool
}

func decodeCartCondition(data []byte, reg *promotion.EvaluatorRegistry) (cartCondition, error) {
	if len(data) == 0 || string(data) == "null" {
		return cartCondition{}, fmt.Errorf("condition config is required")
	}
	var cfg conditionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cartCondition{}, fmt.Errorf("decode: %w", err)
	}
	switch cfg.Type {
	case "min_cart_total":
		if cfg.Value <= 0 {
			return cartCondition{}, fmt.Errorf("min_cart_total value must be positive")
		}
		return cartCondition{typ: cfg.Type, value: cfg.Value}, nil
	default:
		if reg != nil && reg.HasCartCondition(cfg.Type) {
			return cartCondition{typ: cfg.Type, raw: append([]byte(nil), data...), usePlugin: true}, nil
		}
		return cartCondition{}, fmt.Errorf("unknown cart condition type: %q", cfg.Type)
	}
}

func (c cartCondition) matches(ctx context.Context, subtotal shared.Money, reg *promotion.EvaluatorRegistry) (bool, error) {
	if c.usePlugin {
		return reg.EvalCartCondition(ctx, c.typ, c.raw, subtotal)
	}
	switch c.typ {
	case "min_cart_total":
		return subtotal.Amount() >= int64(c.value), nil
	default:
		return false, nil
	}
}

type cartAction struct {
	typ        string
	percentage int
	amount     int64
	raw        []byte
	usePlugin  bool
}

func decodeCartAction(data []byte, reg *promotion.EvaluatorRegistry) (cartAction, error) {
	if len(data) == 0 || string(data) == "null" {
		return cartAction{}, fmt.Errorf("action config is required")
	}
	var cfg actionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cartAction{}, fmt.Errorf("decode: %w", err)
	}
	switch cfg.Type {
	case "percentage":
		if cfg.Percentage <= 0 || cfg.Percentage > 100 {
			return cartAction{}, fmt.Errorf("percentage must be 1-100, got %d", cfg.Percentage)
		}
		return cartAction{typ: cfg.Type, percentage: cfg.Percentage}, nil
	case "fixed":
		if cfg.Amount <= 0 {
			return cartAction{}, fmt.Errorf("fixed amount must be positive")
		}
		return cartAction{typ: cfg.Type, amount: cfg.Amount}, nil
	default:
		if reg != nil && reg.HasCartAction(cfg.Type) {
			return cartAction{typ: cfg.Type, raw: append([]byte(nil), data...), usePlugin: true}, nil
		}
		return cartAction{}, fmt.Errorf("unknown cart action type: %q", cfg.Type)
	}
}

func (a cartAction) compute(ctx context.Context, subtotal shared.Money, currency string, reg *promotion.EvaluatorRegistry) (shared.Money, error) {
	if a.usePlugin {
		return reg.EvalCartAction(ctx, a.typ, a.raw, subtotal, currency)
	}
	switch a.typ {
	case "percentage":
		raw := subtotal.Amount() * int64(a.percentage) / 100
		return shared.NewMoney(raw, currency)
	case "fixed":
		discount, err := shared.NewMoney(a.amount, currency)
		if err != nil {
			return shared.Money{}, err
		}
		if discount.Amount() > subtotal.Amount() {
			return subtotal, nil
		}
		return discount, nil
	default:
		return shared.Money{}, fmt.Errorf("unsupported cart action: %q", a.typ)
	}
}

func itemNetTotal(item domain.PricingItem) shared.Money {
	net := item.Total
	for _, adj := range item.Adjustments {
		if adj.Type == domain.AdjustmentDiscount {
			net = net.Sub(adj.Amount)
		}
	}
	return net
}
