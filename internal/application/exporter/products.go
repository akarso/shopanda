package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// Result holds the summary of an export run.
type Result struct {
	Products  int
	Variants  int
	Skipped   int
	Errors    []string
	RowErrors []exportctx.ExportError
}

// ProductExporter writes products and their variants to CSV.
type ProductExporter struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
	rowHooks *RowHookRunner
}

// NewProductExporter creates a ProductExporter.
func NewProductExporter(products catalog.ProductRepository, variants catalog.VariantRepository) *ProductExporter {
	return &ProductExporter{products: products, variants: variants}
}

// WithRowHooks wires export row hooks invoked before CSV write.
func (exp *ProductExporter) WithRowHooks(registry *exportctx.Registry) *ProductExporter {
	exp.rowHooks = NewRowHookRunner(registry)
	return exp
}

// pageSize controls how many products are fetched per page.
const pageSize = 100

// Export writes all products and their variants to w in CSV format.
//
// CSV columns: name, slug, sku, description, variant_name, plus any attribute
// keys found across all variants. Attribute columns are sorted alphabetically.
func (exp *ProductExporter) Export(ctx context.Context, w io.Writer) (*Result, error) {
	// 1. Collect all products and variants.
	type row struct {
		product catalog.Product
		variant catalog.Variant
	}
	var rows []row
	attrKeys := make(map[string]struct{})

	offset := 0
	for {
		products, err := exp.products.List(ctx, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("export: list products: %w", err)
		}
		if len(products) == 0 {
			break
		}
		for _, p := range products {
			vOffset := 0
			for {
				variants, err := exp.variants.ListByProductID(ctx, p.ID, vOffset, pageSize)
				if err != nil {
					return nil, fmt.Errorf("export: list variants for product %q: %w", p.Slug, err)
				}
				for _, v := range variants {
					rows = append(rows, row{product: p, variant: v})
					for k := range v.Attributes {
						attrKeys[k] = struct{}{}
					}
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

	// 2. Sort attribute keys for deterministic column order.
	sortedAttrs := make([]string, 0, len(attrKeys))
	for k := range attrKeys {
		sortedAttrs = append(sortedAttrs, k)
	}
	sort.Strings(sortedAttrs)

	sort.Strings(sortedAttrs)

	baseHeader := []string{"name", "slug", "sku", "description", "variant_name"}
	baseHeader = append(baseHeader, sortedAttrs...)

	// 3. Run row hooks and collect processed rows.
	type exportRow struct {
		productID string
		row       map[string]string
	}
	processed := make([]exportRow, 0, len(rows))
	result := &Result{}
	rowIndex := 0
	for _, r := range rows {
		rowMap := map[string]string{
			"name":         r.product.Name,
			"slug":         r.product.Slug,
			"sku":          r.variant.SKU,
			"description":  r.product.Description,
			"variant_name": r.variant.Name,
		}
		for k := range attrKeys {
			rowMap[k] = formatAttrValue(r.variant.Attributes[k])
		}
		rowIndex++
		if exp.rowHooks != nil && exp.rowHooks.Enabled() {
			var cont bool
			rowMap, cont = HandleRowHookOutcome(rowIndex, exp.rowHooks.Invoke(ctx, exportctx.EntityProduct, rowIndex, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
			if !cont {
				continue
			}
		}
		processed = append(processed, exportRow{productID: r.product.ID, row: rowMap})
	}

	rowMaps := make([]map[string]string, len(processed))
	for i, p := range processed {
		rowMaps[i] = p.row
	}
	header := MergeExtraColumns(baseHeader, rowMaps)

	// 4. Write CSV.
	writer := csv.NewWriter(w)
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("export: write header: %w", err)
	}

	seen := make(map[string]struct{})
	for _, p := range processed {
		if err := writer.Write(RowToRecord(header, p.row)); err != nil {
			return nil, fmt.Errorf("export: write row: %w", err)
		}
		if _, ok := seen[p.productID]; !ok {
			seen[p.productID] = struct{}{}
			result.Products++
		}
		result.Variants++
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("export: flush csv: %w", err)
	}

	return result, nil
}

// formatAttrValue converts an attribute value to its CSV string representation.
func formatAttrValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}
