package importer

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

// PriceResult holds the summary of a price import run.
type PriceResult struct {
	Created int
	Updated int
	Skipped int
	Errors  []string
}

// txPriceRepo is an optional capability: a PriceRepository that supports
// transaction binding. Satisfied by *postgres.PriceRepo.
type txPriceRepo interface {
	pricing.PriceRepository
	WithTx(tx *sql.Tx) pricing.PriceRepository
}

// txHistoryRepo is an optional capability: a PriceHistoryRepository that
// supports transaction binding. Satisfied by *postgres.PriceHistoryRepo.
type txHistoryRepo interface {
	pricing.PriceHistoryRepository
	WithTx(tx *sql.Tx) pricing.PriceHistoryRepository
}

// PriceImporter imports prices from CSV.
type PriceImporter struct {
	variants  catalog.VariantRepository
	prices    pricing.PriceRepository
	history   pricing.PriceHistoryRepository
	txStarter TxStarter
}

// NewPriceImporter creates a PriceImporter.
// history may be nil; if nil, price snapshots are not recorded.
// txStarter may be nil; if nil and history is non-nil, writes are not wrapped
// in a transaction.
func NewPriceImporter(variants catalog.VariantRepository, prices pricing.PriceRepository, history pricing.PriceHistoryRepository, txStarter TxStarter) *PriceImporter {
	return &PriceImporter{variants: variants, prices: prices, history: history, txStarter: txStarter}
}

// Import reads CSV rows from r and upserts prices.
//
// Required columns: sku, currency, amount.
// Optional column: store_id (defaults to "" for global/default price).
// Each row looks up the variant by SKU, then creates or updates the price
// for that variant+currency+store tuple. Amount is in the smallest currency
// unit (e.g. cents).
func (imp *PriceImporter) Import(ctx context.Context, r io.Reader) (*PriceResult, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("price import: read header: %w", err)
	}

	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(strings.TrimPrefix(h, "\uFEFF")))] = i
	}

	skuIdx, hasSKU := colIdx["sku"]
	currencyIdx, hasCurrency := colIdx["currency"]
	amountIdx, hasAmount := colIdx["amount"]
	storeIDIdx, hasStoreID := colIdx["store_id"]
	if !hasSKU || !hasCurrency || !hasAmount {
		return nil, fmt.Errorf("price import: CSV must have 'sku', 'currency', and 'amount' columns")
	}

	result := &PriceResult{}
	skuCache := make(map[string]*catalog.Variant)
	lineNum := 1 // header is line 1

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		sku := strings.TrimSpace(record[skuIdx])
		if sku == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: empty sku", lineNum))
			result.Skipped++
			continue
		}

		currency := strings.TrimSpace(strings.ToUpper(record[currencyIdx]))
		if !shared.IsValidCurrency(currency) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid currency %q", lineNum, record[currencyIdx]))
			result.Skipped++
			continue
		}

		amountStr := strings.TrimSpace(record[amountIdx])
		amount, err := strconv.ParseInt(amountStr, 10, 64)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid amount %q", lineNum, amountStr))
			result.Skipped++
			continue
		}
		if amount <= 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: amount must be positive, got %d", lineNum, amount))
			result.Skipped++
			continue
		}

		variant, cached := skuCache[sku]
		if !cached {
			variant, err = imp.variants.FindBySKU(ctx, sku)
			if err != nil {
				return nil, fmt.Errorf("price import: find variant by sku %q: %w", sku, err)
			}
			skuCache[sku] = variant
		}
		if variant == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown sku %q", lineNum, sku))
			result.Skipped++
			continue
		}

		money, err := shared.NewMoney(amount, currency)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		storeID := ""
		if hasStoreID {
			storeID = strings.TrimSpace(record[storeIDIdx])
		}

		existing, err := imp.prices.FindByVariantCurrencyAndStore(ctx, variant.ID, currency, storeID)
		if err != nil {
			return nil, fmt.Errorf("price import: find price for sku %q currency %s store %q: %w", sku, currency, storeID, err)
		}

		isUpdate := existing != nil
		priceID := id.New()
		if isUpdate {
			priceID = existing.ID
		}

		p, err := pricing.NewPrice(priceID, variant.ID, storeID, money)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		if err := imp.upsertWithHistory(ctx, &p); err != nil {
			return nil, fmt.Errorf("price import: sku %q currency %s: %w", sku, currency, err)
		}

		if isUpdate {
			result.Updated++
		} else {
			result.Created++
		}
	}

	return result, nil
}

// upsertWithHistory writes the price and records a history snapshot atomically
// when a TxStarter and tx-capable repos are available. Falls back to sequential
// writes otherwise.
func (imp *PriceImporter) upsertWithHistory(ctx context.Context, p *pricing.Price) error {
	if imp.history == nil {
		return imp.prices.Upsert(ctx, p)
	}

	snap, err := pricing.NewPriceSnapshot(id.New(), p.VariantID, p.StoreID, p.Amount)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	if imp.txStarter != nil {
		txP, ok1 := imp.prices.(txPriceRepo)
		txH, ok2 := imp.history.(txHistoryRepo)
		if ok1 && ok2 {
			return imp.upsertInTx(ctx, txP, txH, p, &snap)
		}
	}

	// Fallback: sequential writes.
	if err := imp.prices.Upsert(ctx, p); err != nil {
		return fmt.Errorf("upsert: %w", err)
	}
	if err := imp.history.Record(ctx, &snap); err != nil {
		return fmt.Errorf("record history: %w", err)
	}
	return nil
}

func (imp *PriceImporter) upsertInTx(ctx context.Context, txP txPriceRepo, txH txHistoryRepo, p *pricing.Price, snap *pricing.PriceSnapshot) error {
	tx, err := imp.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	priceRepo := txP.WithTx(tx)
	historyRepo := txH.WithTx(tx)

	if err := priceRepo.Upsert(ctx, p); err != nil {
		tx.Rollback()
		return fmt.Errorf("upsert: %w", err)
	}
	if err := historyRepo.Record(ctx, snap); err != nil {
		tx.Rollback()
		return fmt.Errorf("record history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
