package importer

import (
	"context"
	"database/sql"
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

	type parsedRow struct {
		lineNum     int
		name        string
		slug        string
		sku         string
		desc        string
		variantName string
	}

	// 1. Parse all rows, group by slug
	groups := make(map[string][]parsedRow)
	var allRows []parsedRow
	lineNum := 1
	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Can't parse row, skip
			allRows = append(allRows, parsedRow{lineNum: lineNum})
			continue
		}
		row := parsedRow{
			lineNum:     lineNum,
			name:        colVal(record, colIndex, "name"),
			slug:        colVal(record, colIndex, "slug"),
			sku:         colVal(record, colIndex, "sku"),
			desc:        colVal(record, colIndex, "description"),
			variantName: colVal(record, colIndex, "variant_name"),
		}
		allRows = append(allRows, row)
		groups[row.slug] = append(groups[row.slug], row)
	}

	result := &Result{}
	// 2. Validate all rows (required fields, duplicates, etc)
	for _, row := range allRows {
		if row.name == "" || row.slug == "" || row.sku == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: name, slug, and sku are required", row.lineNum))
			result.Skipped++
		}
	}

	// 3. For each group, validate and write in a transaction
	for slug, rows := range groups {
		// Skip group if any row in group failed required fields
		skip := false
		for _, row := range rows {
			if row.name == "" || row.slug == "" || row.sku == "" {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		// Check if product exists
		existing, err := imp.products.FindBySlug(ctx, slug)
		if err != nil {
			for _, row := range rows {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: find product: %v", row.lineNum, err))
				result.Skipped++
			}
			continue
		}

		// Prepare product and variants
		var product *catalog.Product
		if existing != nil {
			product = existing
		} else {
			p, err := catalog.NewProduct(id.New(), rows[0].name, slug)
			if err != nil {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: new product: %v", row.lineNum, err))
					result.Skipped++
				}
				continue
			}
			p.Description = rows[0].desc
			product = &p
		}

		// Prepare variants
		var variants []catalog.Variant
		for _, row := range rows {
			v, err := catalog.NewVariant(id.New(), product.ID, row.sku)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: new variant: %v", row.lineNum, err))
				result.Skipped++
				skip = true
				break
			}
			v.Name = row.variantName
			variants = append(variants, v)
		}
		if skip {
			continue
		}

		// 4. Write in transaction
		// Only for new products (not for existing)
		if existing == nil {
			// Underlying repo must be able to start a transaction
			txStarter, ok := imp.products.(interface{ DB() *sql.DB })
			if !ok {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: repo does not support transaction", row.lineNum))
					result.Skipped++
				}
				continue
			}
			db := txStarter.DB()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: begin tx: %v", row.lineNum, err))
					result.Skipped++
				}
				continue
			}
			txProducts := imp.products.WithTx(tx)
			txVariants := imp.variants.WithTx(tx)
			// Write product
			if err := txProducts.Create(ctx, product); err != nil {
				tx.Rollback()
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create product: %v", row.lineNum, err))
					result.Skipped++
				}
				continue
			}
			// Write variants
			ok = true
			for i, v := range variants {
				if err := txVariants.Create(ctx, &v); err != nil {
					tx.Rollback()
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create variant: %v", rows[i].lineNum, err))
					result.Skipped++
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			if err := tx.Commit(); err != nil {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: commit: %v", row.lineNum, err))
					result.Skipped++
				}
				continue
			}
			result.Products++
			result.Variants += len(variants)
		} else {
			// Existing product: just add variants (no transaction)
			for i, v := range variants {
				if err := imp.variants.Create(ctx, &v); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create variant: %v", rows[i].lineNum, err))
					result.Skipped++
					continue
				}
				result.Variants++
			}
		}
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
