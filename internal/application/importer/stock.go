package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
)

// StockResult holds the summary of a stock import run.
type StockResult struct {
	Updated   int
	Skipped   int
	Errors    []string
	RowErrors []importctx.ImportError
}

// StockImporter imports stock quantities from CSV.
type StockImporter struct {
	variants catalog.VariantRepository
	stock    inventory.StockRepository
	rowHooks *RowHookRunner

	// reindex is optional — see WithReindex.
	reindex *searchApp.ReindexService
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

// WithReindex enables triggering one scoped reindex per Import call,
// covering every product it actually touched (PR-1049: a bulk CSV import
// publishes no per-row inventory.EventStockUpdated — that would reproduce
// the exact one-job-per-row fan-out risk PR-1036 identified and
// deliberately avoided for bulk price import. A single batched
// ReindexService.Trigger(ScopeProducts{...}) call after the whole import
// completes covers the same ground, and ReindexService's own full-scan-
// threshold/hard-cap logic (PR-1034) already takes over automatically if
// the touched set turns out to be large.). Left unset (the default),
// Import works exactly as before — no reindex triggered, no error.
func (imp *StockImporter) WithReindex(reindex *searchApp.ReindexService) *StockImporter {
	imp.reindex = reindex
	return imp
}

// Import reads CSV rows from r and updates stock quantities.
//
// Required columns: sku, quantity.
// Each row looks up the variant by SKU, then sets the stock quantity.
//
// touchedProductIDs is reindex-triggered via defer, not only on the
// success path: a row partway through the file failing (e.g. SetStock
// erroring on row N) must not leave every already-committed row before it
// permanently unindexed — see StockSyncService.UpsertBySKU's identical
// reasoning for its own chunked writes.
func (imp *StockImporter) Import(ctx context.Context, r io.Reader) (*StockResult, error) {
	touchedProductIDs := make(map[string]struct{})
	defer imp.triggerReindex(ctx, touchedProductIDs)

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
			var cont bool
			rowMap, cont = HandleRowHookOutcome(lineNum, imp.rowHooks.Invoke(ctx, importctx.EntityStock, lineNum, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
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
		touchedProductIDs[variant.ProductID] = struct{}{}
	}

	return result, nil
}

// triggerReindex fires a single scoped reindex covering every product
// this import run touched (deferred by Import — see its own doc comment
// on why this can't just be a plain call at the end) — see WithReindex's
// own doc comment for why this is one batched call, not one per row.
// Best-effort either way: a reindex failure is not worth turning an
// otherwise-successful import into a reported error over (the existing
// manual/scheduled search:reindex remains the fallback, same as any other
// on-save indexing gap in this codebase).
func (imp *StockImporter) triggerReindex(ctx context.Context, touchedProductIDs map[string]struct{}) {
	if imp.reindex == nil || len(touchedProductIDs) == 0 {
		return
	}
	ids := make([]string, 0, len(touchedProductIDs))
	for id := range touchedProductIDs {
		ids = append(ids, id)
	}
	_, _ = imp.reindex.Trigger(ctx, searchApp.ScopeProducts{IDs: ids})
}
