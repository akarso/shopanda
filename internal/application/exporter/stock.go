package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
)

// sanitizeCSVCell escapes values that could be interpreted as spreadsheet
// formulas by prepending a single quote when the cell starts with a
// dangerous character.
func sanitizeCSVCell(s string) string {
	if len(s) == 0 {
		return s
	}
	if strings.ContainsRune("=+-@\t", rune(s[0])) {
		return "'" + s
	}
	return s
}

// StockResult holds the summary of a stock export run.
type StockResult struct {
	Entries   int
	Skipped   int
	Errors    []string
	RowErrors []exportctx.ExportError
}

// StockExporter writes stock entries to CSV.
type StockExporter struct {
	stock    inventory.StockRepository
	variants catalog.VariantRepository
	rowHooks *RowHookRunner
}

// NewStockExporter creates a StockExporter.
func NewStockExporter(stock inventory.StockRepository, variants catalog.VariantRepository) *StockExporter {
	return &StockExporter{stock: stock, variants: variants}
}

// WithRowHooks wires export row hooks invoked before CSV write.
func (exp *StockExporter) WithRowHooks(registry *exportctx.Registry) *StockExporter {
	exp.rowHooks = NewRowHookRunner(registry)
	return exp
}

// Export writes all stock entries to w in CSV format.
//
// CSV columns: sku, quantity.
func (exp *StockExporter) Export(ctx context.Context, w io.Writer) (*StockResult, error) {
	writer := csv.NewWriter(w)

	if err := writer.Write([]string{"sku", "quantity"}); err != nil {
		return nil, fmt.Errorf("stock export: write header: %w", err)
	}

	result := &StockResult{}
	rowIndex := 0
	offset := 0
	for {
		entries, err := exp.stock.ListStock(ctx, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("stock export: list stock: %w", err)
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			variant, err := exp.variants.FindByID(ctx, e.VariantID)
			if err != nil {
				return nil, fmt.Errorf("stock export: find variant %q: %w", e.VariantID, err)
			}
			if variant == nil {
				continue // orphan stock entry, skip
			}
			rowIndex++
			rowMap := map[string]string{
				"sku":      variant.SKU,
				"quantity": strconv.Itoa(e.Quantity),
			}
			if exp.rowHooks != nil && exp.rowHooks.Enabled() {
				var cont bool
				rowMap, cont = HandleRowHookOutcome(rowIndex, exp.rowHooks.Invoke(ctx, exportctx.EntityStock, rowIndex, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
				if !cont {
					continue
				}
			}
			for k, v := range rowMap {
				rowMap[k] = sanitizeCSVCell(v)
			}
			if err := writer.Write(RowToRecord([]string{"sku", "quantity"}, rowMap)); err != nil {
				return nil, fmt.Errorf("stock export: write row: %w", err)
			}
			result.Entries++
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, fmt.Errorf("stock export: flush csv: %w", err)
		}
		if len(entries) < pageSize {
			break
		}
		offset += len(entries)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("stock export: flush csv: %w", err)
	}

	return result, nil
}
