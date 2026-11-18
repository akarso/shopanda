package pluginsdk

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// memPriceRepo is a cloneable in-memory PriceRepository for priceTx tests.
type memPriceRepo struct {
	byKey map[string]*pricing.Price
}

func (m *memPriceRepo) clone() *memPriceRepo {
	out := &memPriceRepo{byKey: map[string]*pricing.Price{}}
	for k, v := range m.byKey {
		cp := *v
		out.byKey[k] = &cp
	}
	return out
}

func (m *memPriceRepo) key(variantID, currency, storeID string) string {
	return variantID + "|" + currency + "|" + storeID
}

func (m *memPriceRepo) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	if m.byKey == nil {
		return nil, nil
	}
	return m.byKey[m.key(variantID, currency, storeID)], nil
}
func (m *memPriceRepo) FindByVariantsCurrencyAndStore(context.Context, []string, string, string) (map[string]*pricing.Price, error) {
	return nil, nil
}
func (m *memPriceRepo) ListByVariantID(context.Context, string) ([]pricing.Price, error) { return nil, nil }
func (m *memPriceRepo) List(context.Context, int, int) ([]pricing.Price, error)          { return nil, nil }
func (m *memPriceRepo) Upsert(_ context.Context, p *pricing.Price) error {
	if m.byKey == nil {
		m.byKey = map[string]*pricing.Price{}
	}
	cp := *p
	m.byKey[m.key(p.VariantID, p.Amount.Currency(), p.StoreID)] = &cp
	return nil
}
func (m *memPriceRepo) WithTx(*sql.Tx) pricing.PriceRepository { return m }

type memHistoryRepo struct {
	snaps  []*pricing.PriceSnapshot
	failOn int // fail the Nth Record (1-based); 0 = never
	n      int
}

func (m *memHistoryRepo) clone() *memHistoryRepo {
	out := &memHistoryRepo{failOn: m.failOn, n: 0}
	for _, s := range m.snaps {
		cp := *s
		out.snaps = append(out.snaps, &cp)
	}
	return out
}

func (m *memHistoryRepo) Record(_ context.Context, snap *pricing.PriceSnapshot) error {
	m.n++
	if m.failOn > 0 && m.n == m.failOn {
		return errors.New("history boom")
	}
	cp := *snap
	m.snaps = append(m.snaps, &cp)
	return nil
}
func (m *memHistoryRepo) LowestSince(context.Context, string, string, string, time.Time) (*pricing.PriceSnapshot, error) {
	return nil, nil
}
func (m *memHistoryRepo) LowestSinceByVariants(context.Context, []string, string, string, time.Time) (map[string]*pricing.PriceSnapshot, error) {
	return nil, nil
}
func (m *memHistoryRepo) WithTx(*sql.Tx) pricing.PriceHistoryRepository { return m }

func TestWritePriceWithHistory_PriceTxRollsBackOnHistoryFailure(t *testing.T) {
	money := shared.MustNewMoney(1000, "PLN")
	basePrices := &memPriceRepo{byKey: map[string]*pricing.Price{
		"v1|PLN|": {ID: "p1", VariantID: "v1", Amount: money},
	}}
	baseHistory := &memHistoryRepo{}

	port := &catalogSeedPort{
		prices:  basePrices,
		history: baseHistory,
		priceTx: func(ctx context.Context, fn func(pricing.PriceRepository, pricing.PriceHistoryRepository) error) error {
			stagedP := basePrices.clone()
			stagedH := baseHistory.clone()
			stagedH.failOn = 1
			if err := fn(stagedP, stagedH); err != nil {
				return err // discard staged — simulates Rollback
			}
			*basePrices = *stagedP
			*baseHistory = *stagedH
			return nil
		},
	}

	updated := shared.MustNewMoney(2500, "PLN")
	price := pricing.Price{ID: "p1", VariantID: "v1", Amount: updated}
	err := port.writePriceWithHistory(context.Background(), &price)
	if err == nil || err.Error() != "history boom" {
		t.Fatalf("err = %v, want history boom", err)
	}
	got := basePrices.byKey["v1|PLN|"]
	if got.Amount.Amount() != 1000 {
		t.Fatalf("upsert not rolled back: amount=%d", got.Amount.Amount())
	}
	if len(baseHistory.snaps) != 0 {
		t.Fatalf("history committed on failure: %#v", baseHistory.snaps)
	}
}

func TestEnsurePrice_PriceTxRollsBackOnHistoryFailure(t *testing.T) {
	money := shared.MustNewMoney(1000, "PLN")
	basePrices := &memPriceRepo{byKey: map[string]*pricing.Price{
		"v1|PLN|": {ID: "p1", VariantID: "v1", Amount: money},
	}}
	baseHistory := &memHistoryRepo{}

	port := &catalogSeedPort{
		prices:  basePrices,
		history: baseHistory,
		priceTx: func(ctx context.Context, fn func(pricing.PriceRepository, pricing.PriceHistoryRepository) error) error {
			stagedP := basePrices.clone()
			stagedH := baseHistory.clone()
			stagedH.failOn = 1
			if err := fn(stagedP, stagedH); err != nil {
				return err
			}
			*basePrices = *stagedP
			*baseHistory = *stagedH
			return nil
		},
	}

	written, err := port.EnsurePrice(context.Background(), CatalogPriceSeed{
		VariantID: "v1", Amount: 2500, Currency: "PLN", Update: true,
	})
	if err == nil || written {
		t.Fatalf("written=%v err=%v, want written=false + history boom", written, err)
	}
	if basePrices.byKey["v1|PLN|"].Amount.Amount() != 1000 {
		t.Fatalf("amount changed despite failed history")
	}
}

func TestEnsurePrice_WrittenFalseWhenSequentialHistoryFails(t *testing.T) {
	// No priceTx: Upsert lands, Record fails — written must still be false.
	money := shared.MustNewMoney(1000, "PLN")
	prices := &memPriceRepo{byKey: map[string]*pricing.Price{
		"v1|PLN|": {ID: "p1", VariantID: "v1", Amount: money},
	}}
	history := &memHistoryRepo{failOn: 1}
	port := &catalogSeedPort{prices: prices, history: history}

	written, err := port.EnsurePrice(context.Background(), CatalogPriceSeed{
		VariantID: "v1", Amount: 2500, Currency: "PLN", Update: true,
	})
	if err == nil || written {
		t.Fatalf("written=%v err=%v", written, err)
	}
	if prices.byKey["v1|PLN|"].Amount.Amount() != 2500 {
		t.Fatalf("expected sequential Upsert to land before Record failure")
	}
}
