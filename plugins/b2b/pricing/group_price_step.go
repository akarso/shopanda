package pricing

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
)

// GroupPriceStep overrides base prices with group-scoped prices when the
// customer belongs to a group.
type GroupPriceStep struct {
	groups customergroup.Repository
	prices customergroup.GroupPriceRepository
}

// NewGroupPriceStep returns a GroupPriceStep.
func NewGroupPriceStep(groups customergroup.Repository, prices customergroup.GroupPriceRepository) *GroupPriceStep {
	if groups == nil {
		panic("b2b group price step: groups repository must not be nil")
	}
	if prices == nil {
		panic("b2b group price step: group price repository must not be nil")
	}
	return &GroupPriceStep{groups: groups, prices: prices}
}

func (s *GroupPriceStep) Name() string { return "b2b.group_price" }

// Apply replaces item unit prices when a group price exists for the customer.
func (s *GroupPriceStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
	customerID, _ := pctx.Meta["customer_id"].(string)
	if customerID == "" || len(pctx.Items) == 0 {
		return nil
	}

	group, err := s.groups.FindGroupByCustomerID(ctx, customerID)
	if err != nil {
		return fmt.Errorf("b2b group price: lookup membership: %w", err)
	}
	if group == nil {
		return nil
	}

	storeID, _ := pctx.Meta["store_id"].(string)
	variantIDs := make([]string, len(pctx.Items))
	for i, item := range pctx.Items {
		variantIDs[i] = item.VariantID
	}

	prices, err := s.prices.FindByVariantsGroupCurrencyAndStore(ctx, variantIDs, group.ID, pctx.Currency, storeID)
	if err != nil {
		return fmt.Errorf("b2b group price: lookup prices: %w", err)
	}

	for i, item := range pctx.Items {
		price := prices[item.VariantID]
		if price == nil {
			continue
		}
		total, err := price.Amount.MulChecked(int64(item.Quantity))
		if err != nil {
			return fmt.Errorf("b2b group price: variant %s: %w", item.VariantID, err)
		}
		pctx.Items[i].UnitPrice = price.Amount
		pctx.Items[i].Total = total
	}
	return nil
}
