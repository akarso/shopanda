package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/id"
)

// Result holds the summary of an import run.
type Result struct {
	Products int
	Variants int
	Skipped  int
	Errors   []string
}

// ProductImporter reads CSV data and persists products and variants.
type ProductImporter struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
}

// NewProductImporter creates a ProductImporter.
func NewProductImporter(products catalog.ProductRepository, variants catalog.VariantRepository) *ProductImporter {
	return &ProductImporter{products: products, variants: variants}
}

// requiredColumns lists the CSV headers that must be present.
var requiredColumns = []string{"name", "slug", "sku"}

// Import reads CSV rows from r and persists them as products and variants.
//
// Expected CSV columns (order does not matter, header row required):
//
//	name        — product name (required)
//	slug        — product slug (required, used to group variants)
//	description — product description (optional)
//	sku         — variant SKU (required)
//	variant_name — variant display name (optional)
//
// Rows sharing the same slug are treated as variants of the same product.
// The first occurrence of a slug defines the product; subsequent rows add variants.
func (imp *ProductImporter) Import(ctx context.Context, r io.Reader) (*Result, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	// Read header.
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("import: read header: %w", err)
	}

	colIndex := make(map[string]int, len(header))
	for i, h := range header {
		colIndex[strings.TrimSpace(strings.ToLower(h))] = i
	}

	// Validate required columns.
	for _, col := range requiredColumns {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("import: missing required column %q", col)
		}
	}

	result := &Result{}
	// slug → product ID, to group variants under the same product.
	slugToProductID := make(map[string]string)
	lineNum := 1 // 1-indexed; header was line 1.

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

		name := colVal(record, colIndex, "name")
		slug := colVal(record, colIndex, "slug")
		sku := colVal(record, colIndex, "sku")

		if name == "" || slug == "" || sku == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: name, slug, and sku are required", lineNum))
			result.Skipped++
			continue
		}

		// Ensure product exists.
		productID, exists := slugToProductID[slug]
		if !exists {
			// Check DB for existing product with this slug.
			existing, err := imp.products.FindBySlug(ctx, slug)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: find product: %v", lineNum, err))
				result.Skipped++
				continue
			}
			if existing != nil {
				productID = existing.ID
				slugToProductID[slug] = productID
			} else {
				// Create product.
				p, err := catalog.NewProduct(id.New(), name, slug)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: new product: %v", lineNum, err))
					result.Skipped++
					continue
				}
				p.Description = colVal(record, colIndex, "description")

				if err := imp.products.Create(ctx, &p); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create product: %v", lineNum, err))
					result.Skipped++
					continue
				}
				productID = p.ID
				slugToProductID[slug] = productID
				result.Products++
			}
		}

		// Create variant.
		v, err := catalog.NewVariant(id.New(), productID, sku)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: new variant: %v", lineNum, err))
			result.Skipped++
			continue
		}
		v.Name = colVal(record, colIndex, "variant_name")

		if err := imp.variants.Create(ctx, &v); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: create variant: %v", lineNum, err))
			result.Skipped++
			continue
		}
		result.Variants++
	}

	return result, nil
}

// colVal returns the trimmed value for a column name, or "" if absent.
func colVal(record []string, colIndex map[string]int, col string) string {
	idx, ok := colIndex[col]
	if !ok || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}
