package pricing

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/b2b/groups"
)

// GroupPriceStep overrides base prices with group-scoped prices when the
// customer belongs to a group.
type GroupPriceStep struct {
	groups groups.Repository
	prices GroupPriceRepository
}

// NewGroupPriceStep returns a GroupPriceStep.
func NewGroupPriceStep(groupRepo groups.Repository, prices GroupPriceRepository) *GroupPriceStep {
	if groupRepo == nil {
		panic("b2b group price step: groups repository must not be nil")
	}
	if prices == nil {
		panic("b2b group price step: group price repository must not be nil")
	}
	return &GroupPriceStep{groups: groupRepo, prices: prices}
}

func (s *GroupPriceStep) Name() string { return "b2b.group_price" }

// Apply replaces item unit prices when a group price exists for the customer.
func (s *GroupPriceStep) Apply(ctx context.Context, pctx extapi.PricingContext) error {
	customerIDRaw, _ := pctx.MetaGet("customer_id")
	customerID, _ := customerIDRaw.(string)
	items := pctx.Items()
	if customerID == "" || len(items) == 0 {
		return nil
	}

	group, err := s.groups.FindGroupByCustomerID(ctx, customerID)
	if err != nil {
		return fmt.Errorf("b2b group price: lookup membership: %w", err)
	}
	if group == nil {
		return nil
	}

	storeIDRaw, _ := pctx.MetaGet("store_id")
	storeID, _ := storeIDRaw.(string)
	variantIDs := make([]string, len(items))
	for i, item := range items {
		variantIDs[i] = item.VariantID
	}

	prices, err := s.prices.FindByVariantsGroupCurrencyAndStore(ctx, variantIDs, group.ID, pctx.Currency(), storeID)
	if err != nil {
		return fmt.Errorf("b2b group price: lookup prices: %w", err)
	}

	for i, item := range items {
		price := prices[item.VariantID]
		if price == nil {
			continue
		}
		total, err := price.Amount.MulChecked(int64(item.Quantity))
		if err != nil {
			return fmt.Errorf("b2b group price: variant %s: %w", item.VariantID, err)
		}
		if err := pctx.SetItemPrice(i, price.Amount, total); err != nil {
			return fmt.Errorf("b2b group price: variant %s: %w", item.VariantID, err)
		}
	}
	return nil
}

var _ extapi.PricingStep = (*GroupPriceStep)(nil)
