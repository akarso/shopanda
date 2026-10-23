package importer

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// PriceResult holds the summary of a price import run.
type PriceResult struct {
	Created   int
	Updated   int
	Skipped   int
	Errors    []string
	RowErrors []importctx.ImportError
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

// lockingProductRepo locks type and pricing_mode on the transaction-bound repo.
type lockingProductRepo interface {
	catalog.ProductRepository
	LockTypeAndModes(ctx context.Context, id string) (catalog.Type, catalog.PricingMode, catalog.StockMode, bool, error)
}

// rowRejectError is a per-row validation failure. Import records it and
// continues. It is not an infrastructure failure.
type rowRejectError struct {
	msg string
}

func (e *rowRejectError) Error() string { return e.msg }

// PriceImporter imports prices from CSV.
type PriceImporter struct {
	variants  catalog.VariantRepository
	products  catalog.ProductRepository
	prices    pricing.PriceRepository
	history   pricing.PriceHistoryRepository
	txStarter TxStarter
	bus       *event.Bus
	rowHooks  *RowHookRunner
}

// NewPriceImporter creates a PriceImporter.
// history may be nil; if nil, price snapshots are not recorded.
// txStarter may be nil; if nil and history is non-nil, writes are not wrapped
// in a transaction.
// bus may be nil; if nil, no events are published.
func NewPriceImporter(variants catalog.VariantRepository, prices pricing.PriceRepository, history pricing.PriceHistoryRepository, txStarter TxStarter, bus *event.Bus) *PriceImporter {
	return &PriceImporter{variants: variants, prices: prices, history: history, txStarter: txStarter, bus: bus}
}

// WithRowHooks wires import row hooks invoked after header validation and before persist.
func (imp *PriceImporter) WithRowHooks(registry *importctx.Registry) *PriceImporter {
	imp.rowHooks = NewRowHookRunner(registry)
	return imp
}

// WithProducts rejects price rows for grouped parents and dynamic bundles.
// Import returns an error when this was not set.
func (imp *PriceImporter) WithProducts(products catalog.ProductRepository) *PriceImporter {
	imp.products = products
	return imp
}

// Import reads CSV rows from r and upserts prices.
//
// Required columns: sku, currency, amount.
// Optional column: store_id (defaults to "" for global/default price).
// Each row looks up the variant by SKU, then creates or updates the price
// for that variant+currency+store tuple. Amount is in the smallest currency
// unit (e.g. cents).
func (imp *PriceImporter) Import(ctx context.Context, r io.Reader) (*PriceResult, error) {
	if imp.products == nil {
		return nil, fmt.Errorf("price import: product repository is required")
	}
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

	_, hasSKU := colIdx["sku"]
	_, hasCurrency := colIdx["currency"]
	_, hasAmount := colIdx["amount"]
	_, hasStoreID := colIdx["store_id"]
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

		rowMap := RecordToRow(record, colIdx)
		if imp.rowHooks != nil {
			var cont bool
			rowMap, cont = HandleRowHookOutcome(lineNum, imp.rowHooks.Invoke(ctx, importctx.EntityPrice, lineNum, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
			if !cont {
				continue
			}
		}

		sku := colValRow(rowMap, "sku")
		if sku == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: empty sku", lineNum))
			result.Skipped++
			continue
		}

		currency := strings.ToUpper(colValRow(rowMap, "currency"))
		if !shared.IsValidCurrency(currency) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid currency %q", lineNum, rowMap["currency"]))
			result.Skipped++
			continue
		}

		amountStr := colValRow(rowMap, "amount")
		amount, err := strconv.ParseInt(amountStr, 10, 64)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid amount %q", lineNum, amountStr))
			result.Skipped++
			continue
		}
		if amount < 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: amount must be non-negative, got %d", lineNum, amount))
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
		product, err := imp.products.FindByID(catalog.WithOperatorProductReadScope(ctx), variant.ProductID)
		if err != nil {
			return nil, fmt.Errorf("price import: find product for sku %q: %w", sku, err)
		}
		if product == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: product not found for sku %q", lineNum, sku))
			result.Skipped++
			continue
		}
		if product.Type == catalog.TypeGrouped {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, catalog.GroupedPriceRejectedMessage))
			result.Skipped++
			continue
		}
		if product.Type == catalog.TypeBundle && product.PricingMode == catalog.PricingModeDynamic {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, catalog.DynamicBundlePriceRejectedMessage))
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
			storeID = colValRow(rowMap, "store_id")
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

		if err := imp.upsertWithHistory(ctx, &p, variant.ProductID); err != nil {
			var rejected *rowRejectError
			if errors.As(err, &rejected) {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, rejected.Error()))
				result.Skipped++
				continue
			}
			return nil, fmt.Errorf("price import: sku %q currency %s: %w", sku, currency, err)
		}

		if imp.bus != nil {
			_ = imp.bus.Publish(ctx, event.New(pricing.EventPriceUpserted, "price.importer", pricing.PriceUpsertedData{
				PriceID:   p.ID,
				VariantID: p.VariantID,
				ProductID: variant.ProductID,
				StoreID:   p.StoreID,
				Currency:  p.Amount.Currency(),
				Amount:    p.Amount.Amount(),
			}))
		}

		if isUpdate {
			result.Updated++
		} else {
			result.Created++
		}
	}

	return result, nil
}

// upsertWithHistory locks the product row and writes the price (and history,
// when configured) on that same transaction. Grouped and dynamic bundles are
// rejected from the locked type and pricing_mode, matching admin price PUT.
func (imp *PriceImporter) upsertWithHistory(ctx context.Context, p *pricing.Price, productID string) error {
	if imp.txStarter == nil {
		return fmt.Errorf("transaction starter is required")
	}
	txProducts, ok := imp.products.(txProductRepo)
	if !ok {
		return fmt.Errorf("product repository does not support transactions")
	}
	txPrices, ok := imp.prices.(txPriceRepo)
	if !ok {
		return fmt.Errorf("price repository does not support transactions")
	}
	var txHistory txHistoryRepo
	if imp.history != nil {
		txHistory, ok = imp.history.(txHistoryRepo)
		if !ok {
			return fmt.Errorf("price history repository does not support transactions")
		}
	}

	tx, err := imp.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	locker, ok := txProducts.WithTx(tx).(lockingProductRepo)
	if !ok {
		return fmt.Errorf("product repository does not support row locks")
	}
	productType, pricingMode, _, _, err := locker.LockTypeAndModes(ctx, productID)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return err
		}
		return fmt.Errorf("lock product: %w", err)
	}
	if productType == catalog.TypeGrouped {
		return &rowRejectError{msg: catalog.GroupedPriceRejectedMessage}
	}
	if productType == catalog.TypeBundle && pricingMode == catalog.PricingModeDynamic {
		return &rowRejectError{msg: catalog.DynamicBundlePriceRejectedMessage}
	}

	if err := txPrices.WithTx(tx).Upsert(ctx, p); err != nil {
		return fmt.Errorf("upsert: %w", err)
	}
	if txHistory != nil {
		snap, err := pricing.NewPriceSnapshot(id.New(), p.VariantID, p.StoreID, p.Amount)
		if err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
		if err := txHistory.WithTx(tx).Record(ctx, &snap); err != nil {
			return fmt.Errorf("record history: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
