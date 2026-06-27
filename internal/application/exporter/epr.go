package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
)

// EprResult holds the summary of an EPR export run.
type EprResult struct {
	Rows int
}

// EprExportOptions controls EPR CSV export behavior.
type EprExportOptions struct {
	StoreID string
	// IncludeEmpty when false skips variant rows with no EPR packaging data.
	IncludeEmpty bool
}

// EprExporter writes EPR packaging metadata per SKU for merchant reporting tools.
type EprExporter struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
	config   legal.ConfigGetter
}

// NewEprExporter creates an EprExporter.
func NewEprExporter(products catalog.ProductRepository, variants catalog.VariantRepository, config legal.ConfigGetter) *EprExporter {
	return &EprExporter{products: products, variants: variants, config: config}
}

var eprCSVHeader = []string{
	"sku",
	"product_slug",
	"product_name",
	"packaging_material",
	"packaging_weight_g",
	"recyclable",
	"recycled_content_pct",
	"scheme_registration_id",
}

// Export writes EPR packaging rows to w in CSV format.
func (exp *EprExporter) Export(ctx context.Context, w io.Writer, opts EprExportOptions) (*EprResult, error) {
	storeScheme := ""
	if exp.config != nil {
		scheme, err := legal.StoreSchemeRegistrationID(ctx, exp.config, opts.StoreID)
		if err != nil {
			return nil, fmt.Errorf("epr export: store scheme: %w", err)
		}
		storeScheme = scheme
	}

	writer := csv.NewWriter(w)
	if err := writer.Write(eprCSVHeader); err != nil {
		return nil, fmt.Errorf("epr export: write header: %w", err)
	}

	result := &EprResult{}
	offset := 0
	for {
		products, err := exp.products.List(ctx, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("epr export: list products: %w", err)
		}
		if len(products) == 0 {
			break
		}
		for _, p := range products {
			vOffset := 0
			for {
				variants, err := exp.variants.ListByProductID(ctx, p.ID, vOffset, pageSize)
				if err != nil {
					return nil, fmt.Errorf("epr export: list variants for %q: %w", p.Slug, err)
				}
				for _, v := range variants {
					packaging := legal.ParseEprPackaging(p.Attributes, v.Attributes)
					if !opts.IncludeEmpty && !packaging.HasData() {
						continue
					}
					packaging = packaging.WithStoreSchemeID(storeScheme)
					record := []string{
						v.SKU,
						p.Slug,
						p.Name,
						packaging.Material,
						formatEprNumber(packaging.WeightG),
						formatEprBool(packaging.Recyclable),
						formatEprNumber(packaging.RecycledContentPct),
						packaging.SchemeRegistrationID,
					}
					if err := writer.Write(record); err != nil {
						return nil, fmt.Errorf("epr export: write row: %w", err)
					}
					result.Rows++
				}
				if len(variants) < pageSize {
					break
				}
				vOffset += len(variants)
			}
		}
		if len(products) < pageSize {
			break
		}
		offset += len(products)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("epr export: flush csv: %w", err)
	}
	return result, nil
}

func formatEprBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func formatEprNumber(v float64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
