package pricing_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
	b2bpricing "github.com/akarso/shopanda/plugins/b2b/pricing"
)

type stubGroups struct {
	group *customergroup.Group
}

func (s *stubGroups) List(context.Context, int, int) ([]customergroup.Group, error) { return nil, nil }
func (s *stubGroups) FindByID(context.Context, string) (*customergroup.Group, error) {
	return nil, nil
}
func (s *stubGroups) FindByCode(context.Context, string) (*customergroup.Group, error) {
	return nil, nil
}
func (s *stubGroups) Save(context.Context, *customergroup.Group) error { return nil }
func (s *stubGroups) Delete(context.Context, string) error             { return nil }
func (s *stubGroups) AssignCustomer(context.Context, string, string) error {
	return nil
}
func (s *stubGroups) RemoveCustomer(context.Context, string) error { return nil }
func (s *stubGroups) FindGroupByCustomerID(_ context.Context, customerID string) (*customergroup.Group, error) {
	if customerID == "" {
		return nil, nil
	}
	return s.group, nil
}

type stubGroupPrices struct {
	byVariant map[string]*customergroup.GroupPrice
}

func (s *stubGroupPrices) FindByVariantsGroupCurrencyAndStore(_ context.Context, variantIDs []string, _, _, _ string) (map[string]*customergroup.GroupPrice, error) {
	out := make(map[string]*customergroup.GroupPrice)
	for _, vid := range variantIDs {
		if p := s.byVariant[vid]; p != nil {
			out[vid] = p
		}
	}
	return out, nil
}

func (s *stubGroupPrices) FindExactByVariantGroupCurrencyAndStore(_ context.Context, variantID, _, _, _ string) (*customergroup.GroupPrice, error) {
	return s.byVariant[variantID], nil
}

func (s *stubGroupPrices) FindByVariantGroupCurrencyAndStore(_ context.Context, variantID, _, _, storeID string) (*customergroup.GroupPrice, error) {
	return s.byVariant[variantID], nil
}

func (s *stubGroupPrices) Upsert(context.Context, *customergroup.GroupPrice) error { return nil }
func (s *stubGroupPrices) Delete(context.Context, string) error                    { return nil }

func TestGroupPriceStep_OverridesBasePrice(t *testing.T) {
	group, err := customergroup.NewGroup(id.New(), "wholesale", "Wholesale", "")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	groupAmount, err := shared.NewMoney(800, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	groupPrice, err := customergroup.NewGroupPrice(id.New(), group.ID, "variant-1", "", groupAmount)
	if err != nil {
		t.Fatalf("NewGroupPrice: %v", err)
	}

	step := b2bpricing.NewGroupPriceStep(
		&stubGroups{group: &group},
		&stubGroupPrices{byVariant: map[string]*customergroup.GroupPrice{"variant-1": &groupPrice}},
	)

	baseAmount, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	item, err := pricing.NewPricingItem("variant-1", 2, baseAmount)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}
	pctx.Meta["customer_id"] = id.New()

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if pctx.Items[0].UnitPrice.Amount() != 800 {
		t.Fatalf("UnitPrice = %d, want 800", pctx.Items[0].UnitPrice.Amount())
	}
	if pctx.Items[0].Total.Amount() != 1600 {
		t.Fatalf("Total = %d, want 1600", pctx.Items[0].Total.Amount())
	}
}

func TestGroupPriceStep_NoCustomerNoOp(t *testing.T) {
	step := b2bpricing.NewGroupPriceStep(&stubGroups{}, &stubGroupPrices{})
	baseAmount, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	item, err := pricing.NewPricingItem("variant-1", 1, baseAmount)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if pctx.Items[0].UnitPrice.Amount() != 1000 {
		t.Fatalf("UnitPrice = %d, want unchanged 1000", pctx.Items[0].UnitPrice.Amount())
	}
}
