#!/usr/bin/env python3
"""Generate PR-020 files: pipeline executor, finalize step, base price step + tests."""
import os

BASE = os.path.dirname(os.path.abspath(__file__))

files = {}

# --- 1. Pipeline executor ---
files["internal/domain/pricing/pipeline.go"] = '''\
package pricing

import (
\t"context"
\t"fmt"
)

// Pipeline runs pricing steps sequentially against a PricingContext.
type Pipeline struct {
\tsteps []PricingStep
}

// NewPipeline creates a Pipeline from the given steps.
func NewPipeline(steps ...PricingStep) Pipeline {
\treturn Pipeline{steps: steps}
}

// Execute runs each step in order. An error from any step halts the pipeline.
func (p Pipeline) Execute(ctx context.Context, pctx *PricingContext) error {
\tfor _, step := range p.steps {
\t\tif err := step.Apply(ctx, pctx); err != nil {
\t\t\treturn fmt.Errorf("pipeline: step %q: %w", step.Name(), err)
\t\t}
\t}
\treturn nil
}
'''

# --- 2. Finalize step ---
files["internal/domain/pricing/finalize_step.go"] = '''\
package pricing

import (
\t"context"

\t"github.com/akarso/shopanda/internal/domain/shared"
)

// FinalizeStep computes aggregate totals on a PricingContext.
type FinalizeStep struct{}

// NewFinalizeStep returns a new FinalizeStep.
func NewFinalizeStep() *FinalizeStep {
\treturn &FinalizeStep{}
}

func (s *FinalizeStep) Name() string { return "finalize" }

// Apply sums item totals into Subtotal, aggregates adjustments by type,
// and computes GrandTotal = Subtotal - DiscountsTotal + TaxTotal + FeesTotal.
// Accumulators are reset to zero so calling Apply twice is idempotent.
func (s *FinalizeStep) Apply(_ context.Context, pctx *PricingContext) error {
\tzero := shared.MustZero(pctx.Currency)

\tsubtotal := zero
\tfor _, item := range pctx.Items {
\t\tsubtotal = subtotal.Add(item.Total)
\t}
\tpctx.Subtotal = subtotal

\tdiscounts := zero
\ttaxes := zero
\tfees := zero

\tfor _, item := range pctx.Items {
\t\tfor _, adj := range item.Adjustments {
\t\t\tswitch adj.Type {
\t\t\tcase AdjustmentDiscount:
\t\t\t\tdiscounts = discounts.Add(adj.Amount)
\t\t\tcase AdjustmentTax:
\t\t\t\ttaxes = taxes.Add(adj.Amount)
\t\t\tcase AdjustmentFee:
\t\t\t\tfees = fees.Add(adj.Amount)
\t\t\t}
\t\t}
\t}

\tfor _, adj := range pctx.Adjustments {
\t\tswitch adj.Type {
\t\tcase AdjustmentDiscount:
\t\t\tdiscounts = discounts.Add(adj.Amount)
\t\tcase AdjustmentTax:
\t\t\ttaxes = taxes.Add(adj.Amount)
\t\tcase AdjustmentFee:
\t\t\tfees = fees.Add(adj.Amount)
\t\t}
\t}

\tpctx.DiscountsTotal = discounts
\tpctx.TaxTotal = taxes
\tpctx.FeesTotal = fees

\tpctx.GrandTotal = subtotal.Sub(discounts).Add(taxes).Add(fees)
\treturn nil
}
'''

# --- 3. Base price step ---
os.makedirs(os.path.join(BASE, "internal/application/pricing"), exist_ok=True)

files["internal/application/pricing/base_price_step.go"] = '''\
package pricing

import (
\t"context"
\t"fmt"

\tdomain "github.com/akarso/shopanda/internal/domain/pricing"
)

// BasePriceStep populates item prices from the price repository.
type BasePriceStep struct {
\tprices domain.PriceRepository
}

// NewBasePriceStep returns a new BasePriceStep.
func NewBasePriceStep(prices domain.PriceRepository) *BasePriceStep {
\treturn &BasePriceStep{prices: prices}
}

func (s *BasePriceStep) Name() string { return "base" }

// Apply looks up the base price for each item and sets UnitPrice and Total.
func (s *BasePriceStep) Apply(ctx context.Context, pctx *domain.PricingContext) error {
\tfor i, item := range pctx.Items {
\t\tprice, err := s.prices.FindByVariantAndCurrency(ctx, item.VariantID, pctx.Currency)
\t\tif err != nil {
\t\t\treturn fmt.Errorf("base price: variant %s: %w", item.VariantID, err)
\t\t}
\t\tif price == nil {
\t\t\treturn fmt.Errorf("base price: no price for variant %s in %s", item.VariantID, pctx.Currency)
\t\t}
\t\ttotal, err := price.Amount.MulChecked(int64(item.Quantity))
\t\tif err != nil {
\t\t\treturn fmt.Errorf("base price: variant %s: %w", item.VariantID, err)
\t\t}
\t\tpctx.Items[i].UnitPrice = price.Amount
\t\tpctx.Items[i].Total = total
\t}
\treturn nil
}
'''

# --- 4. Pipeline tests ---
files["internal/domain/pricing/pipeline_test.go"] = '''\
package pricing_test

import (
\t"context"
\t"errors"
\t"testing"

\t"github.com/akarso/shopanda/internal/domain/pricing"
)

type stubStep struct {
\tname string
\tfn   func(*pricing.PricingContext) error
}

func (s *stubStep) Name() string                                              { return s.name }
func (s *stubStep) Apply(_ context.Context, ctx *pricing.PricingContext) error { return s.fn(ctx) }

func TestPipelineExecute_NoSteps(t *testing.T) {
\tp := pricing.NewPipeline()
\tpctx, err := pricing.NewPricingContext("EUR")
\tif err != nil {
\t\tt.Fatalf("new context: %v", err)
\t}
\tif err := p.Execute(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("execute: %v", err)
\t}
}

func TestPipelineExecute_SingleStep(t *testing.T) {
\tcalled := false
\tstep := &stubStep{name: "test", fn: func(ctx *pricing.PricingContext) error {
\t\tcalled = true
\t\treturn nil
\t}}
\tp := pricing.NewPipeline(step)
\tpctx, _ := pricing.NewPricingContext("EUR")
\tif err := p.Execute(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("execute: %v", err)
\t}
\tif !called {
\t\tt.Fatal("step was not called")
\t}
}

func TestPipelineExecute_StepOrder(t *testing.T) {
\tvar order []string
\ts1 := &stubStep{name: "first", fn: func(ctx *pricing.PricingContext) error {
\t\torder = append(order, "first")
\t\treturn nil
\t}}
\ts2 := &stubStep{name: "second", fn: func(ctx *pricing.PricingContext) error {
\t\torder = append(order, "second")
\t\treturn nil
\t}}
\tp := pricing.NewPipeline(s1, s2)
\tpctx, _ := pricing.NewPricingContext("EUR")
\tif err := p.Execute(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("execute: %v", err)
\t}
\tif len(order) != 2 || order[0] != "first" || order[1] != "second" {
\t\tt.Fatalf("order = %v, want [first second]", order)
\t}
}

func TestPipelineExecute_StopOnError(t *testing.T) {
\tcalled := false
\ts1 := &stubStep{name: "fail", fn: func(ctx *pricing.PricingContext) error {
\t\treturn errors.New("boom")
\t}}
\ts2 := &stubStep{name: "skip", fn: func(ctx *pricing.PricingContext) error {
\t\tcalled = true
\t\treturn nil
\t}}
\tp := pricing.NewPipeline(s1, s2)
\tpctx, _ := pricing.NewPricingContext("EUR")
\terr := p.Execute(context.Background(), &pctx)
\tif err == nil {
\t\tt.Fatal("expected error")
\t}
\tif called {
\t\tt.Fatal("second step should not have been called")
\t}
}

func TestPipelineExecute_ErrorWrapsStepName(t *testing.T) {
\tstep := &stubStep{name: "broken", fn: func(ctx *pricing.PricingContext) error {
\t\treturn errors.New("whoops")
\t}}
\tp := pricing.NewPipeline(step)
\tpctx, _ := pricing.NewPricingContext("EUR")
\terr := p.Execute(context.Background(), &pctx)
\tif err == nil {
\t\tt.Fatal("expected error")
\t}
\twant := `pipeline: step "broken": whoops`
\tif err.Error() != want {
\t\tt.Errorf("error = %q, want %q", err.Error(), want)
\t}
}
'''

# --- 5. Finalize step tests ---
files["internal/domain/pricing/finalize_step_test.go"] = '''\
package pricing_test

import (
\t"context"
\t"testing"

\t"github.com/akarso/shopanda/internal/domain/pricing"
\t"github.com/akarso/shopanda/internal/domain/shared"
)

func TestFinalizeStep_Name(t *testing.T) {
\ts := pricing.NewFinalizeStep()
\tif s.Name() != "finalize" {
\t\tt.Errorf("Name() = %q, want %q", s.Name(), "finalize")
\t}
}

func TestFinalizeStep_SubtotalFromItems(t *testing.T) {
\tpctx, _ := pricing.NewPricingContext("EUR")
\titem1, _ := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(500, "EUR"))
\titem2, _ := pricing.NewPricingItem("v2", 1, shared.MustNewMoney(300, "EUR"))
\tpctx.Items = []pricing.PricingItem{item1, item2}

\ts := pricing.NewFinalizeStep()
\tif err := s.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("apply: %v", err)
\t}
\tif pctx.Subtotal.Amount() != 1300 {
\t\tt.Errorf("Subtotal = %d, want 1300", pctx.Subtotal.Amount())
\t}
\tif pctx.GrandTotal.Amount() != 1300 {
\t\tt.Errorf("GrandTotal = %d, want 1300", pctx.GrandTotal.Amount())
\t}
}

func TestFinalizeStep_EmptyItems(t *testing.T) {
\tpctx, _ := pricing.NewPricingContext("EUR")
\ts := pricing.NewFinalizeStep()
\tif err := s.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("apply: %v", err)
\t}
\tif pctx.Subtotal.Amount() != 0 {
\t\tt.Errorf("Subtotal = %d, want 0", pctx.Subtotal.Amount())
\t}
\tif pctx.GrandTotal.Amount() != 0 {
\t\tt.Errorf("GrandTotal = %d, want 0", pctx.GrandTotal.Amount())
\t}
}

func TestFinalizeStep_WithContextAdjustments(t *testing.T) {
\tpctx, _ := pricing.NewPricingContext("EUR")
\titem, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "EUR"))
\tpctx.Items = []pricing.PricingItem{item}

\tdiscount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "PROMO", shared.MustNewMoney(100, "EUR"))
\ttax, _ := pricing.NewAdjustment(pricing.AdjustmentTax, "VAT", shared.MustNewMoney(180, "EUR"))
\tfee, _ := pricing.NewAdjustment(pricing.AdjustmentFee, "SHIP", shared.MustNewMoney(50, "EUR"))
\tpctx.Adjustments = []pricing.Adjustment{discount, tax, fee}

\ts := pricing.NewFinalizeStep()
\tif err := s.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("apply: %v", err)
\t}
\tif pctx.Subtotal.Amount() != 1000 {
\t\tt.Errorf("Subtotal = %d, want 1000", pctx.Subtotal.Amount())
\t}
\tif pctx.DiscountsTotal.Amount() != 100 {
\t\tt.Errorf("DiscountsTotal = %d, want 100", pctx.DiscountsTotal.Amount())
\t}
\tif pctx.TaxTotal.Amount() != 180 {
\t\tt.Errorf("TaxTotal = %d, want 180", pctx.TaxTotal.Amount())
\t}
\tif pctx.FeesTotal.Amount() != 50 {
\t\tt.Errorf("FeesTotal = %d, want 50", pctx.FeesTotal.Amount())
\t}
\t// GrandTotal = 1000 - 100 + 180 + 50 = 1130
\tif pctx.GrandTotal.Amount() != 1130 {
\t\tt.Errorf("GrandTotal = %d, want 1130", pctx.GrandTotal.Amount())
\t}
}

func TestFinalizeStep_WithItemAdjustments(t *testing.T) {
\tpctx, _ := pricing.NewPricingContext("EUR")
\titem, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "EUR"))
\tdiscount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "ITEM10", shared.MustNewMoney(100, "EUR"))
\titem.Adjustments = []pricing.Adjustment{discount}
\tpctx.Items = []pricing.PricingItem{item}

\ts := pricing.NewFinalizeStep()
\tif err := s.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("apply: %v", err)
\t}
\tif pctx.DiscountsTotal.Amount() != 100 {
\t\tt.Errorf("DiscountsTotal = %d, want 100", pctx.DiscountsTotal.Amount())
\t}
\t// GrandTotal = 1000 - 100 = 900
\tif pctx.GrandTotal.Amount() != 900 {
\t\tt.Errorf("GrandTotal = %d, want 900", pctx.GrandTotal.Amount())
\t}
}

func TestFinalizeStep_Idempotent(t *testing.T) {
\tpctx, _ := pricing.NewPricingContext("EUR")
\titem, _ := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(500, "EUR"))
\tdiscount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "PROMO", shared.MustNewMoney(50, "EUR"))
\tpctx.Items = []pricing.PricingItem{item}
\tpctx.Adjustments = []pricing.Adjustment{discount}

\ts := pricing.NewFinalizeStep()

\t// Apply twice -- totals must be identical.
\tif err := s.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("first apply: %v", err)
\t}
\tif err := s.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("second apply: %v", err)
\t}
\tif pctx.Subtotal.Amount() != 1000 {
\t\tt.Errorf("Subtotal = %d, want 1000", pctx.Subtotal.Amount())
\t}
\tif pctx.DiscountsTotal.Amount() != 50 {
\t\tt.Errorf("DiscountsTotal = %d, want 50", pctx.DiscountsTotal.Amount())
\t}
\t// GrandTotal = 1000 - 50 = 950
\tif pctx.GrandTotal.Amount() != 950 {
\t\tt.Errorf("GrandTotal = %d, want 950", pctx.GrandTotal.Amount())
\t}
}
'''

# --- 6. Base price step tests ---
files["internal/application/pricing/base_price_step_test.go"] = '''\
package pricing_test

import (
\t"context"
\t"errors"
\t"testing"
\t"time"

\t"github.com/akarso/shopanda/internal/application/pricing"
\tdomain "github.com/akarso/shopanda/internal/domain/pricing"
\t"github.com/akarso/shopanda/internal/domain/shared"
)

type mockPriceRepo struct {
\tprices map[string]map[string]*domain.Price
\terr    error
}

func (m *mockPriceRepo) FindByVariantAndCurrency(_ context.Context, variantID, currency string) (*domain.Price, error) {
\tif m.err != nil {
\t\treturn nil, m.err
\t}
\tif byVariant, ok := m.prices[variantID]; ok {
\t\tif p, ok := byVariant[currency]; ok {
\t\t\treturn p, nil
\t\t}
\t}
\treturn nil, nil
}

func (m *mockPriceRepo) ListByVariantID(_ context.Context, _ string) ([]domain.Price, error) {
\treturn nil, nil
}

func (m *mockPriceRepo) Upsert(_ context.Context, _ *domain.Price) error {
\treturn nil
}

func makeMockRepo(entries ...mockEntry) *mockPriceRepo {
\trepo := &mockPriceRepo{prices: make(map[string]map[string]*domain.Price)}
\tfor _, e := range entries {
\t\tif _, ok := repo.prices[e.variantID]; !ok {
\t\t\trepo.prices[e.variantID] = make(map[string]*domain.Price)
\t\t}
\t\tm := shared.MustNewMoney(e.amount, e.currency)
\t\trepo.prices[e.variantID][e.currency] = &domain.Price{
\t\t\tID:        "price-" + e.variantID + "-" + e.currency,
\t\t\tVariantID: e.variantID,
\t\t\tAmount:    m,
\t\t\tCreatedAt: time.Now().UTC(),
\t\t}
\t}
\treturn repo
}

type mockEntry struct {
\tvariantID string
\tcurrency  string
\tamount    int64
}

func TestBasePriceStep_Name(t *testing.T) {
\tstep := pricing.NewBasePriceStep(&mockPriceRepo{})
\tif step.Name() != "base" {
\t\tt.Errorf("Name() = %q, want %q", step.Name(), "base")
\t}
}

func TestBasePriceStep_PopulatesPrices(t *testing.T) {
\trepo := makeMockRepo(
\t\tmockEntry{"v1", "EUR", 500},
\t\tmockEntry{"v2", "EUR", 300},
\t)
\tstep := pricing.NewBasePriceStep(repo)

\tpctx, _ := domain.NewPricingContext("EUR")
\titem1, _ := domain.NewPricingItem("v1", 2, shared.MustNewMoney(0, "EUR"))
\titem2, _ := domain.NewPricingItem("v2", 1, shared.MustNewMoney(0, "EUR"))
\tpctx.Items = []domain.PricingItem{item1, item2}

\tif err := step.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("apply: %v", err)
\t}
\tif pctx.Items[0].UnitPrice.Amount() != 500 {
\t\tt.Errorf("Items[0].UnitPrice = %d, want 500", pctx.Items[0].UnitPrice.Amount())
\t}
\tif pctx.Items[0].Total.Amount() != 1000 {
\t\tt.Errorf("Items[0].Total = %d, want 1000", pctx.Items[0].Total.Amount())
\t}
\tif pctx.Items[1].UnitPrice.Amount() != 300 {
\t\tt.Errorf("Items[1].UnitPrice = %d, want 300", pctx.Items[1].UnitPrice.Amount())
\t}
\tif pctx.Items[1].Total.Amount() != 300 {
\t\tt.Errorf("Items[1].Total = %d, want 300", pctx.Items[1].Total.Amount())
\t}
}

func TestBasePriceStep_NoPriceFound(t *testing.T) {
\trepo := &mockPriceRepo{prices: make(map[string]map[string]*domain.Price)}
\tstep := pricing.NewBasePriceStep(repo)

\tpctx, _ := domain.NewPricingContext("EUR")
\titem, _ := domain.NewPricingItem("v1", 1, shared.MustNewMoney(0, "EUR"))
\tpctx.Items = []domain.PricingItem{item}

\terr := step.Apply(context.Background(), &pctx)
\tif err == nil {
\t\tt.Fatal("expected error for missing price")
\t}
}

func TestBasePriceStep_RepoError(t *testing.T) {
\trepo := &mockPriceRepo{err: errors.New("db down")}
\tstep := pricing.NewBasePriceStep(repo)

\tpctx, _ := domain.NewPricingContext("EUR")
\titem, _ := domain.NewPricingItem("v1", 1, shared.MustNewMoney(0, "EUR"))
\tpctx.Items = []domain.PricingItem{item}

\terr := step.Apply(context.Background(), &pctx)
\tif err == nil {
\t\tt.Fatal("expected error from repo")
\t}
}

func TestBasePriceStep_EmptyItems(t *testing.T) {
\trepo := &mockPriceRepo{prices: make(map[string]map[string]*domain.Price)}
\tstep := pricing.NewBasePriceStep(repo)

\tpctx, _ := domain.NewPricingContext("EUR")
\tif err := step.Apply(context.Background(), &pctx); err != nil {
\t\tt.Fatalf("apply: %v", err)
\t}
}
'''

for rel, content in files.items():
    path = os.path.join(BASE, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
    print(f"wrote {rel}")

print("done")
