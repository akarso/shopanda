package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// DownloadableFileExporter writes downloadable product → asset links as CSV.
// Columns: product_slug, asset_id, position, label.
type DownloadableFileExporter struct {
	products catalog.ProductRepository
	files    *catalogApp.DownloadableFileService
}

// NewDownloadableFileExporter creates a downloadable-file CSV exporter.
func NewDownloadableFileExporter(products catalog.ProductRepository, files *catalogApp.DownloadableFileService) *DownloadableFileExporter {
	if products == nil {
		panic("exporter: downloadable file exporter: product repository must not be nil")
	}
	if files == nil {
		panic("exporter: downloadable file exporter: file service must not be nil")
	}
	return &DownloadableFileExporter{products: products, files: files}
}

// Export lists downloadable products under operator scope and writes one row
// per attachment. Metadata only — binary blobs are not exported.
func (exp *DownloadableFileExporter) Export(ctx context.Context, w io.Writer) (*CompositionResult, error) {
	if exp.products == nil || exp.files == nil {
		return nil, fmt.Errorf("downloadable file export: product repository and file service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"product_slug", "asset_id", "position", "label"}); err != nil {
		return nil, fmt.Errorf("downloadable file export: write header: %w", err)
	}
	result := &CompositionResult{}
	lineNum := 1
	if err := eachProduct(ctx, exp.products, catalog.TypeDownloadable, func(p catalog.Product) error {
		rows, err := exp.files.List(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("downloadable file export: list %q: %w", p.Slug, err)
		}
		for _, row := range rows {
			lineNum++
			if p.Slug == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: product %q has no slug", lineNum, p.ID))
				result.Skipped++
				continue
			}
			if err := writer.Write([]string{p.Slug, row.AssetID, strconv.Itoa(row.Position), row.Label}); err != nil {
				return fmt.Errorf("downloadable file export: write row: %w", err)
			}
			result.Rows++
		}
		return nil
	}); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("downloadable file export: flush csv: %w", err)
	}
	return result, nil
}
