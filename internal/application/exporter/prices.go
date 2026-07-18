package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

// PriceResult holds the summary of a price export run.
type PriceResult struct {
	Entries   int
	Skipped   int
	Errors    []string
	RowErrors []exportctx.ExportError
}

// PriceExporter writes prices to CSV.
type PriceExporter struct {
	prices   pricing.PriceRepository
	variants catalog.VariantRepository
	rowHooks *RowHookRunner
}

// NewPriceExporter creates a PriceExporter.
func NewPriceExporter(prices pricing.PriceRepository, variants catalog.VariantRepository) *PriceExporter {
	return &PriceExporter{prices: prices, variants: variants}
}

// WithRowHooks wires export row hooks invoked before CSV write.
func (exp *PriceExporter) WithRowHooks(registry *exportctx.Registry) *PriceExporter {
	exp.rowHooks = NewRowHookRunner(registry)
	return exp
}

// Export writes all prices to w in CSV format.
//
// CSV columns: sku, currency, amount, store_id.
func (exp *PriceExporter) Export(ctx context.Context, w io.Writer) (*PriceResult, error) {
	writer := csv.NewWriter(w)

	if err := writer.Write([]string{"sku", "currency", "amount", "store_id"}); err != nil {
		return nil, fmt.Errorf("price export: write header: %w", err)
	}

	result := &PriceResult{}
	variantCache := make(map[string]*catalog.Variant)
	rowIndex := 0
	offset := 0
	for {
		prices, err := exp.prices.List(ctx, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("price export: list prices: %w", err)
		}
		if len(prices) == 0 {
			break
		}
		for _, p := range prices {
			variant, cached := variantCache[p.VariantID]
			if !cached {
				variant, err = exp.variants.FindByID(ctx, p.VariantID)
				if err != nil {
					return nil, fmt.Errorf("price export: find variant %q: %w", p.VariantID, err)
				}
				variantCache[p.VariantID] = variant
			}
			if variant == nil {
				continue // orphan price entry, skip
			}
			rowIndex++
			rowMap := map[string]string{
				"sku":      variant.SKU,
				"currency": p.Amount.Currency(),
				"amount":   strconv.FormatInt(p.Amount.Amount(), 10),
				"store_id": p.StoreID,
			}
			if exp.rowHooks != nil && exp.rowHooks.Enabled() {
				var cont bool
				rowMap, cont = HandleRowHookOutcome(rowIndex, exp.rowHooks.Invoke(ctx, exportctx.EntityPrice, rowIndex, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
				if !cont {
					continue
				}
			}
			for k, v := range rowMap {
				rowMap[k] = sanitizeCSVCell(v)
			}
			if err := writer.Write(RowToRecord([]string{"sku", "currency", "amount", "store_id"}, rowMap)); err != nil {
				return nil, fmt.Errorf("price export: write row: %w", err)
			}
			result.Entries++
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, fmt.Errorf("price export: flush csv: %w", err)
		}
		if len(prices) < pageSize {
			break
		}
		offset += len(prices)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("price export: flush csv: %w", err)
	}

	return result, nil
}
