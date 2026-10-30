package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// DownloadableFileImporter adds downloadable product → asset links from CSV.
// Columns: product_slug, asset_id, position, label.
// position/label are optional headers: when absent on re-import, the matching
// join's Position/Label are retained; when present, CSV values win (empty
// position means 0). Omitted rows are not deletes.
// Assets must already exist under private/downloads/ (Attach enforces this).
type DownloadableFileImporter struct {
	products catalog.ProductRepository
	files    *catalogApp.DownloadableFileService
}

// NewDownloadableFileImporter creates a downloadable-file CSV importer.
func NewDownloadableFileImporter(products catalog.ProductRepository, files *catalogApp.DownloadableFileService) *DownloadableFileImporter {
	if products == nil {
		panic("importer: downloadable file importer: product repository must not be nil")
	}
	if files == nil {
		panic("importer: downloadable file importer: file service must not be nil")
	}
	return &DownloadableFileImporter{products: products, files: files}
}

// Import resolves product slugs and attaches each row. Lookup/attach errors
// are recorded as line N and the import continues.
func (imp *DownloadableFileImporter) Import(ctx context.Context, r io.Reader) (*CompositionResult, error) {
	if imp.products == nil || imp.files == nil {
		return nil, fmt.Errorf("downloadable file import: product repository and file service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("downloadable file import: read header: %w", err)
	}
	colIdx := compositionColIndex(header)
	for _, col := range []string{"product_slug", "asset_id"} {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("downloadable file import: missing required column %q", col)
		}
	}
	_, hasPosition := colIdx["position"]
	_, hasLabel := colIdx["label"]

	result := &CompositionResult{}
	cache := map[string]*catalog.Product{}
	lineNum := 1
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
		row := RecordToRow(record, colIdx)
		productSlug := colValRow(row, "product_slug")
		assetID := colValRow(row, "asset_id")
		if productSlug == "" || assetID == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: product_slug and asset_id are required", lineNum))
			result.Skipped++
			continue
		}
		product, err := cachedProductBySlug(ctx, imp.products, cache, productSlug)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: find product slug %q: %v", lineNum, productSlug, err))
			result.Skipped++
			continue
		}
		if product == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown product slug %q", lineNum, productSlug))
			result.Skipped++
			continue
		}
		existing, listErr := imp.files.List(ctx, product.ID)
		if listErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, compositionErrText(listErr)))
			result.Skipped++
			continue
		}
		var match *catalog.DownloadableFile
		for i := range existing {
			if existing[i].AssetID == assetID {
				match = &existing[i]
				break
			}
		}
		f, err := catalog.NewDownloadableFile(product.ID, assetID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, compositionErrText(err)))
			result.Skipped++
			continue
		}
		// Retain matching join metadata when optional CSV columns are absent.
		if match != nil {
			f.Position = match.Position
			f.Label = match.Label
		}
		if hasPosition {
			position := 0
			if raw := colValRow(row, "position"); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 0 {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: position must be an integer >= 0", lineNum))
					result.Skipped++
					continue
				}
				position = n
			}
			f.Position = position
		}
		if hasLabel {
			f.Label = colValRow(row, "label")
		}
		if err := imp.files.Attach(ctx, f); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, compositionErrText(err)))
			result.Skipped++
			continue
		}
		if match != nil {
			result.Updated++
		} else {
			result.Added++
		}
	}
	return result, nil
}
