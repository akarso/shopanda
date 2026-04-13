package pricing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/pricing"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

type mockPriceRepo struct {
	prices map[string]map[string]*domain.Price
	err    error
}

func (m *mockPriceRepo) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, _ string) (*domain.Price, error) {
	if m.err != nil {
		return nil, m.err
	}
	if byVariant, ok := m.prices[variantID]; ok {
		if p, ok := byVariant[currency]; ok {
			return p, nil
		}
	}
	return nil, nil
}

func (m *mockPriceRepo) ListByVariantID(_ context.Context, _ string) ([]domain.Price, error) {
	return nil, nil
}

func (m *mockPriceRepo) List(_ context.Context, _, _ int) ([]domain.Price, error) {
	return nil, nil
}

func (m *mockPriceRepo) Upsert(_ context.Context, _ *domain.Price) error {
	return nil
}

func makeMockRepo(entries ...mockEntry) *mockPriceRepo {
	repo := &mockPriceRepo{prices: make(map[string]map[string]*domain.Price)}
	for _, e := range entries {
		if _, ok := repo.prices[e.variantID]; !ok {
			repo.prices[e.variantID] = make(map[string]*domain.Price)
		}
		m := shared.MustNewMoney(e.amount, e.currency)
		repo.prices[e.variantID][e.currency] = &domain.Price{
			ID:        "price-" + e.variantID + "-" + e.currency,
			VariantID: e.variantID,
			Amount:    m,
			CreatedAt: time.Now().UTC(),
		}
	}
	return repo
}

type mockEntry struct {
	variantID string
	currency  string
	amount    int64
}

func TestBasePriceStep_Name(t *testing.T) {
	step := pricing.NewBasePriceStep(&mockPriceRepo{})
	if step.Name() != "base" {
		t.Errorf("Name() = %q, want %q", step.Name(), "base")
	}
}

func TestBasePriceStep_PopulatesPrices(t *testing.T) {
	repo := makeMockRepo(
		mockEntry{"v1", "EUR", 500},
		mockEntry{"v2", "EUR", 300},
	)
	step := pricing.NewBasePriceStep(repo)

	pctx, _ := domain.NewPricingContext("EUR")
	item1, _ := domain.NewPricingItem("v1", 2, shared.MustNewMoney(0, "EUR"))
	item2, _ := domain.NewPricingItem("v2", 1, shared.MustNewMoney(0, "EUR"))
	pctx.Items = []domain.PricingItem{item1, item2}

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if pctx.Items[0].UnitPrice.Amount() != 500 {
		t.Errorf("Items[0].UnitPrice = %d, want 500", pctx.Items[0].UnitPrice.Amount())
	}
	if pctx.Items[0].Total.Amount() != 1000 {
		t.Errorf("Items[0].Total = %d, want 1000", pctx.Items[0].Total.Amount())
	}
	if pctx.Items[1].UnitPrice.Amount() != 300 {
		t.Errorf("Items[1].UnitPrice = %d, want 300", pctx.Items[1].UnitPrice.Amount())
	}
	if pctx.Items[1].Total.Amount() != 300 {
		t.Errorf("Items[1].Total = %d, want 300", pctx.Items[1].Total.Amount())
	}
}

func TestBasePriceStep_NoPriceFound(t *testing.T) {
	repo := &mockPriceRepo{prices: make(map[string]map[string]*domain.Price)}
	step := pricing.NewBasePriceStep(repo)

	pctx, _ := domain.NewPricingContext("EUR")
	item, _ := domain.NewPricingItem("v1", 1, shared.MustNewMoney(0, "EUR"))
	pctx.Items = []domain.PricingItem{item}

	err := step.Apply(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error for missing price")
	}
}

func TestBasePriceStep_RepoError(t *testing.T) {
	repo := &mockPriceRepo{err: errors.New("db down")}
	step := pricing.NewBasePriceStep(repo)

	pctx, _ := domain.NewPricingContext("EUR")
	item, _ := domain.NewPricingItem("v1", 1, shared.MustNewMoney(0, "EUR"))
	pctx.Items = []domain.PricingItem{item}

	err := step.Apply(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error from repo")
	}
}

func TestBasePriceStep_EmptyItems(t *testing.T) {
	repo := &mockPriceRepo{prices: make(map[string]map[string]*domain.Price)}
	step := pricing.NewBasePriceStep(repo)

	pctx, _ := domain.NewPricingContext("EUR")
	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
}
