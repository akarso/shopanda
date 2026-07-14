package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
)

// StockResult holds the summary of a stock import run.
type StockResult struct {
	Updated int
	Skipped int
	Errors  []string
}

// StockImporter imports stock quantities from CSV.
type StockImporter struct {
	variants catalog.VariantRepository
	stock    inventory.StockRepository
	rowHooks *RowHookRunner
}

// NewStockImporter creates a StockImporter.
func NewStockImporter(variants catalog.VariantRepository, stock inventory.StockRepository) *StockImporter {
	return &StockImporter{variants: variants, stock: stock}
}

// WithRowHooks wires import row hooks invoked after header validation and before persist.
func (imp *StockImporter) WithRowHooks(registry *importctx.Registry) *StockImporter {
	imp.rowHooks = NewRowHookRunner(registry)
	return imp
}

// Import reads CSV rows from r and updates stock quantities.
//
// Required columns: sku, quantity.
// Each row looks up the variant by SKU, then sets the stock quantity.
func (imp *StockImporter) Import(ctx context.Context, r io.Reader) (*StockResult, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("stock import: read header: %w", err)
	}

	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	_, hasSKU := colIdx["sku"]
	_, hasQty := colIdx["quantity"]
	if !hasSKU || !hasQty {
		return nil, fmt.Errorf("stock import: CSV must have 'sku' and 'quantity' columns")
	}

	result := &StockResult{}
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
			var hookErr error
			rowMap, hookErr = imp.rowHooks.Invoke(ctx, importctx.EntityStock, lineNum, rowMap)
			if hookErr != nil {
				result.Errors = append(result.Errors, RowHookError(lineNum, hookErr))
				result.Skipped++
				continue
			}
		}

		sku := colValRow(rowMap, "sku")
		if sku == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: empty sku", lineNum))
			result.Skipped++
			continue
		}

		qtyStr := colValRow(rowMap, "quantity")
		qty, err := strconv.Atoi(qtyStr)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid quantity %q", lineNum, qtyStr))
			result.Skipped++
			continue
		}
		if qty < 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: negative quantity %d", lineNum, qty))
			result.Skipped++
			continue
		}

		variant, err := imp.variants.FindBySKU(ctx, sku)
		if err != nil {
			return nil, fmt.Errorf("stock import: find variant by sku %q: %w", sku, err)
		}
		if variant == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown sku %q", lineNum, sku))
			result.Skipped++
			continue
		}

		entry, err := inventory.NewStockEntry(variant.ID, qty)
		if err != nil {
			return nil, fmt.Errorf("stock import: new stock entry: %w", err)
		}
		if err := imp.stock.SetStock(ctx, &entry); err != nil {
			return nil, fmt.Errorf("stock import: set stock for sku %q: %w", sku, err)
		}
		result.Updated++
	}

	return result, nil
}
