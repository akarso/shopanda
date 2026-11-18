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

// WithTx returns the active work copy for a recordingSeedStarter tx; otherwise self.
func (m *memPriceRepo) WithTx(tx catalogSeedTx) pricing.PriceRepository {
	if ft, ok := tx.(*recordingSeedTx); ok && ft.s.workPrices != nil {
		return ft.s.workPrices
	}
	return m
}

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
func (m *memHistoryRepo) WithTx(tx catalogSeedTx) pricing.PriceHistoryRepository {
	if ft, ok := tx.(*recordingSeedTx); ok && ft.s.workHistory != nil {
		return ft.s.workHistory
	}
	return m
}

// recordingSeedStarter is a catalogSeedTxStarter that stages price/history
// writes until Commit (Rollback discards). Used to prove production priceTx /
// Import wiring via NewCatalogSeedPort.
type recordingSeedStarter struct {
	prices  *memPriceRepo
	history *memHistoryRepo

	workPrices  *memPriceRepo
	workHistory *memHistoryRepo

	begins, commits, rollbacks int
	failHistoryOn              int // set on workHistory when beginning a tx
}

type recordingSeedTx struct{ s *recordingSeedStarter }

func (s *recordingSeedStarter) BeginTx(context.Context, *sql.TxOptions) (catalogSeedTx, error) {
	s.begins++
	s.workPrices = s.prices.clone()
	s.workHistory = s.history.clone()
	s.workHistory.failOn = s.failHistoryOn
	s.workHistory.n = 0
	return &recordingSeedTx{s: s}, nil
}

func (t *recordingSeedTx) Commit() error {
	t.s.commits++
	*t.s.prices = *t.s.workPrices
	*t.s.history = *t.s.workHistory
	t.s.workPrices = nil
	t.s.workHistory = nil
	return nil
}

func (t *recordingSeedTx) Rollback() error {
	t.s.rollbacks++
	t.s.workPrices = nil
	t.s.workHistory = nil
	return nil
}

func stagingPriceTx(baseP *memPriceRepo, baseH *memHistoryRepo, failHistoryOn int) catalogSeedPriceTx {
	return func(ctx context.Context, fn func(pricing.PriceRepository, pricing.PriceHistoryRepository) error) error {
		stagedP := baseP.clone()
		stagedH := baseH.clone()
		stagedH.failOn = failHistoryOn
		if err := fn(stagedP, stagedH); err != nil {
			return err
		}
		*baseP = *stagedP
		*baseH = *stagedH
		return nil
	}
}

func TestWritePriceWithHistory_PriceTxRollsBackOnHistoryFailure(t *testing.T) {
	money := shared.MustNewMoney(1000, "PLN")
	basePrices := &memPriceRepo{byKey: map[string]*pricing.Price{
		"v1|PLN|": {ID: "p1", VariantID: "v1", Amount: money},
	}}
	baseHistory := &memHistoryRepo{}
	port := &catalogSeedPort{
		prices:  basePrices,
		history: baseHistory,
		priceTx: stagingPriceTx(basePrices, baseHistory, 1),
	}

	updated := shared.MustNewMoney(2500, "PLN")
	price := pricing.Price{ID: "p1", VariantID: "v1", Amount: updated}
	err := port.writePriceWithHistory(context.Background(), &price)
	if err == nil || err.Error() != "history boom" {
		t.Fatalf("err = %v, want history boom", err)
	}
	if basePrices.byKey["v1|PLN|"].Amount.Amount() != 1000 {
		t.Fatalf("upsert not rolled back: amount=%d", basePrices.byKey["v1|PLN|"].Amount.Amount())
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
		priceTx: stagingPriceTx(basePrices, baseHistory, 1),
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

func TestNewCatalogSeedPort_PriceTxRollbackOnHistoryFailure(t *testing.T) {
	money := shared.MustNewMoney(1000, "PLN")
	prices := &memPriceRepo{byKey: map[string]*pricing.Price{
		"v1|PLN|": {ID: "p1", VariantID: "v1", Amount: money},
	}}
	history := &memHistoryRepo{}
	starter := &recordingSeedStarter{prices: prices, history: history, failHistoryOn: 1}

	port, err := NewCatalogSeedPort(nil, nil, prices, history, nil, starter)
	if err != nil {
		t.Fatalf("NewCatalogSeedPort: %v", err)
	}

	written, err := port.EnsurePrice(context.Background(), CatalogPriceSeed{
		VariantID: "v1", Amount: 2500, Currency: "PLN", Update: true,
	})
	if err == nil || written {
		t.Fatalf("written=%v err=%v", written, err)
	}
	if starter.begins != 1 || starter.rollbacks != 1 || starter.commits != 0 {
		t.Fatalf("tx counts begin=%d commit=%d rollback=%d", starter.begins, starter.commits, starter.rollbacks)
	}
	if prices.byKey["v1|PLN|"].Amount.Amount() != 1000 {
		t.Fatalf("base amount changed: %d", prices.byKey["v1|PLN|"].Amount.Amount())
	}
}

func TestNewCatalogSeedPort_FailsClosedWithoutWithTx(t *testing.T) {
	// Interface wrappers without WithTx — must not silently fall back to sequential.
	prices := struct{ pricing.PriceRepository }{&memPriceRepo{}}
	history := struct{ pricing.PriceHistoryRepository }{&memHistoryRepo{}}
	starter := &recordingSeedStarter{prices: &memPriceRepo{}, history: &memHistoryRepo{}}

	_, err := NewCatalogSeedPort(nil, nil, prices, history, nil, starter)
	if err == nil {
		t.Fatal("expected error when db set without WithTx")
	}
}

func TestImport_HistoryFailureRollsBackOuterTx(t *testing.T) {
	money := shared.MustNewMoney(1000, "PLN")
	prices := &memPriceRepo{byKey: map[string]*pricing.Price{
		"v1|PLN|": {ID: "p1", VariantID: "v1", Amount: money},
	}}
	history := &memHistoryRepo{}
	starter := &recordingSeedStarter{prices: prices, history: history, failHistoryOn: 1}

	port, err := NewCatalogSeedPort(nil, nil, prices, history, nil, starter)
	if err != nil {
		t.Fatalf("NewCatalogSeedPort: %v", err)
	}

	_, err = port.Import(context.Background(), CatalogSeedBatch{
		Prices: []CatalogPriceSeed{{
			VariantID: "v1", Amount: 3333, Currency: "PLN", Update: true,
		}},
	})
	if err == nil {
		t.Fatal("expected Import error")
	}
	if starter.begins != 1 || starter.rollbacks != 1 || starter.commits != 0 {
		t.Fatalf("tx counts begin=%d commit=%d rollback=%d", starter.begins, starter.commits, starter.rollbacks)
	}
	if prices.byKey["v1|PLN|"].Amount.Amount() != 1000 {
		t.Fatalf("Import left amount=%d after history failure", prices.byKey["v1|PLN|"].Amount.Amount())
	}
}
