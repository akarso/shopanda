package importer

import (
	"context"
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

// PriceImporter imports prices from CSV.
type PriceImporter struct {
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
}

// NewPriceImporter creates a PriceImporter.
func NewPriceImporter(variants catalog.VariantRepository, prices pricing.PriceRepository) *PriceImporter {
	return &PriceImporter{variants: variants, prices: prices}
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

		if err := imp.prices.Upsert(ctx, &p); err != nil {
			return nil, fmt.Errorf("price import: upsert price for sku %q currency %s: %w", sku, currency, err)
		}

		if isUpdate {
			result.Updated++
		} else {
			result.Created++
		}
	}

	return result, nil
}
