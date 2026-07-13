package cartdemo_test

import (
	"context"
	"io"
	"testing"
	"time"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/hooks"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/testutil"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type stubCartRepo struct {
	carts map[string]*domainCart.Cart
}

func newStubCartRepo() *stubCartRepo {
	return &stubCartRepo{carts: make(map[string]*domainCart.Cart)}
}

func (r *stubCartRepo) FindByID(_ context.Context, id string) (*domainCart.Cart, error) {
	c, ok := r.carts[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	clone.Items = make([]domainCart.Item, len(c.Items))
	copy(clone.Items, c.Items)
	return &clone, nil
}

func (r *stubCartRepo) FindActiveByCustomerID(_ context.Context, customerID string) (*domainCart.Cart, error) {
	for _, c := range r.carts {
		if c.CustomerID == customerID && c.IsActive() {
			clone := *c
			clone.Items = make([]domainCart.Item, len(c.Items))
			copy(clone.Items, c.Items)
			return &clone, nil
		}
	}
	return nil, nil
}

func (r *stubCartRepo) Save(_ context.Context, c *domainCart.Cart) error {
	clone := *c
	clone.Items = make([]domainCart.Item, len(c.Items))
	copy(clone.Items, c.Items)
	r.carts[c.ID] = &clone
	return nil
}

func (r *stubCartRepo) Delete(_ context.Context, id string) error {
	delete(r.carts, id)
	return nil
}

func (r *stubCartRepo) FindRecoveryCandidates(_ context.Context, _ time.Time, _ int) ([]*domainCart.Cart, error) {
	return nil, nil
}

func (r *stubCartRepo) MarkRecoveryEmailSent(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}

type stubPriceRepo struct {
	prices map[string]*pricing.Price
}

func newStubPriceRepo() *stubPriceRepo {
	return &stubPriceRepo{prices: make(map[string]*pricing.Price)}
}

func (r *stubPriceRepo) set(variantID, currency string, amount int64) {
	key := variantID + ":" + currency + ":"
	p, _ := pricing.NewPrice("price-"+key, variantID, "", shared.MustNewMoney(amount, currency))
	r.prices[key] = &p
}

func (r *stubPriceRepo) FindByVariantCurrencyAndStore(ctx context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	if p := r.prices[variantID+":"+currency+":"+storeID]; p != nil {
		return p, nil
	}
	return r.prices[variantID+":"+currency+":"], nil
}

func (r *stubPriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, r.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

func (r *stubPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error {
	return nil
}

func testLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func testBus() *event.Bus {
	return event.NewBus(testLogger())
}

func testPipeline(prices pricing.PriceRepository) pricing.Pipeline {
	return pricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		pricing.NewFinalizeStep(),
	)
}

type noopPricingStep struct {
	name string
}

func (s *noopPricingStep) Name() string { return s.name }

func (s *noopPricingStep) Apply(_ context.Context, _ *pricing.PricingContext) error {
	return nil
}

func buildCartDemoPipeline(prices pricing.PriceRepository, app *plugin.App) pricing.Pipeline {
	core := []pricing.PricingStep{
		appPricing.NewBasePriceStep(prices),
		&noopPricingStep{name: "catalog_promotions"},
		&noopPricingStep{name: "cart_promotions"},
		pricing.NewFinalizeStep(),
	}
	regs := make([]appPricing.PluginStepRegistration, 0, len(app.PricingStepRegistrations()))
	for _, reg := range app.PricingStepRegistrations() {
		step, ok := reg.Step.(pricing.PricingStep)
		if !ok {
			continue
		}
		regs = append(regs, appPricing.PluginStepRegistration{Step: step, Position: reg.Position})
	}
	steps, err := appPricing.MergePluginSteps(core, regs)
	if err != nil {
		panic(err)
	}
	return pricing.NewPipeline(steps...)
}

func newCartServiceWithHooks(t *testing.T, hookReg *hooks.Registry) (*cartApp.Service, *stubPriceRepo) {
	t.Helper()
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus(), nil, hookReg)
	return svc, prices
}

func newCartServiceWithHooksAndPluginSteps(t *testing.T, hookReg *hooks.Registry, app *plugin.App) (*cartApp.Service, *stubPriceRepo) {
	t.Helper()
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	pipeline := buildCartDemoPipeline(prices, app)
	svc := cartApp.NewService(carts, prices, nil, nil, pipeline, testLogger(), testBus(), nil, hookReg)
	return svc, prices
}
