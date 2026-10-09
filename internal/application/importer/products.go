package importer

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/id"
)

// Result holds the summary of an import run.
type Result struct {
	Products  int
	Variants  int
	Skipped   int
	Errors    []string
	RowErrors []importctx.ImportError
}

// TxStarter begins database transactions.
type TxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// txProductRepo is an optional capability: a ProductRepository that supports
// transaction binding. Satisfied by *postgres.ProductRepo.
type txProductRepo interface {
	catalog.ProductRepository
	WithTx(tx *sql.Tx) catalog.ProductRepository
}

// txVariantRepo is an optional capability: a VariantRepository that supports
// transaction binding. Satisfied by *postgres.VariantRepo.
type txVariantRepo interface {
	catalog.VariantRepository
	WithTx(tx *sql.Tx) catalog.VariantRepository
}

// ProductImporter reads CSV data and persists products and variants.
type ProductImporter struct {
	products  catalog.ProductRepository
	variants  catalog.VariantRepository
	txStarter TxStarter
	registry  *catalog.AttributeRegistry
	groupCode string
	rowHooks  *RowHookRunner
}

// NewProductImporter creates a ProductImporter.
// txStarter may be nil; if nil, writes are not wrapped in a transaction.
func NewProductImporter(products catalog.ProductRepository, variants catalog.VariantRepository, txStarter TxStarter) *ProductImporter {
	return &ProductImporter{products: products, variants: variants, txStarter: txStarter}
}

// WithAttributeValidation sets an attribute registry and group code for
// validating attribute values during import. Returns the importer for chaining.
func (imp *ProductImporter) WithAttributeValidation(registry *catalog.AttributeRegistry, groupCode string) *ProductImporter {
	imp.registry = registry
	imp.groupCode = groupCode
	return imp
}

// WithRowHooks wires import row hooks invoked after header validation and before persist.
func (imp *ProductImporter) WithRowHooks(registry *importctx.Registry) *ProductImporter {
	imp.rowHooks = NewRowHookRunner(registry)
	return imp
}

// requiredColumns lists the CSV headers that must be present.
var requiredColumns = []string{"name", "slug", "sku"}

// knownColumns are CSV headers handled by the core import logic.
// Any other header is treated as an attribute column.
//
// "type" is reserved for product taxonomy (PR-1052). A CSV that previously
// used a free-form attribute column named "type" must rename that attribute;
// values in the type column are never stored in variant.Attributes.
var knownColumns = map[string]struct{}{
	"name": {}, "slug": {}, "sku": {}, "description": {}, "variant_name": {}, "type": {},
}

// Import reads CSV rows from r and persists them as products and variants.
//
// Expected CSV columns (order does not matter, header row required):
//
//	name         — product name (required)
//	slug         — product slug (required, used to group variants)
//	description  — product description (optional)
//	type         — product type (optional; empty → simple on create; case-sensitive like admin API)
//	sku          — variant SKU (required)
//	variant_name — variant display name (optional)
//
// Any additional columns are treated as attribute values and stored in
// variant.Attributes. When an AttributeRegistry is configured via
// WithAttributeValidation, values are parsed according to their declared
// type and validated against the specified group.
//
// Rows sharing the same slug are treated as variants of the same product.
// The first occurrence of a slug defines the product; subsequent rows add variants.
// Type consensus: all-empty → simple on create (and no type check on existing);
// when any cell is set, blanks count as simple so empty+virtual conflicts.
// Type updates on existing products are rejected (create-only for this field).
func (imp *ProductImporter) Import(ctx context.Context, r io.Reader) (*Result, error) {
	ctx = catalog.WithIncludeNonActiveProducts(ctx)
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

	// Validate required columns in header when row hooks are disabled.
	if imp.rowHooks == nil || !imp.rowHooks.Enabled() {
		for _, col := range requiredColumns {
			if _, ok := colIndex[col]; !ok {
				return nil, fmt.Errorf("import: missing required column %q", col)
			}
		}
	}

	// Identify attribute columns (any header not in the known set).
	var attrColumns []string
	for col := range colIndex {
		if _, ok := knownColumns[col]; !ok {
			attrColumns = append(attrColumns, col)
		}
	}

	// 1. Parse all rows, group by slug
	groups := make(map[string][]productImportRow)
	var allRows []productImportRow
	result := &Result{}
	lineNum := 1
	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Can't parse row, skip
			allRows = append(allRows, productImportRow{lineNum: lineNum})
			continue
		}
		rowMap := RecordToRow(record, colIndex)
		if imp.rowHooks != nil {
			var cont bool
			rowMap, cont = HandleRowHookOutcome(lineNum, imp.rowHooks.Invoke(ctx, importctx.EntityProduct, lineNum, rowMap), &result.Skipped, &result.Errors, &result.RowErrors)
			if !cont {
				continue
			}
		}
		row := productImportRow{
			lineNum:     lineNum,
			name:        colValRow(rowMap, "name"),
			slug:        colValRow(rowMap, "slug"),
			sku:         colValRow(rowMap, "sku"),
			desc:        colValRow(rowMap, "description"),
			productType: colValRow(rowMap, "type"), // case-sensitive; matches admin API
			variantName: colValRow(rowMap, "variant_name"),
		}
		if len(attrColumns) > 0 {
			raw := make(map[string]string, len(attrColumns))
			for col, v := range rowMap {
				if _, known := knownColumns[col]; known || v == "" {
					continue
				}
				raw[col] = v
			}
			if len(raw) > 0 {
				row.rawAttrs = raw
			}
		}
		allRows = append(allRows, row)
		groups[row.slug] = append(groups[row.slug], row)
	}

	// 2. Validate all rows (required fields, type)
	failedLines := make(map[int]struct{})
	for _, row := range allRows {
		if row.name == "" || row.slug == "" || row.sku == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: name, slug, and sku are required", row.lineNum))
			result.Skipped++
			failedLines[row.lineNum] = struct{}{}
			continue
		}
		if row.productType != "" {
			typ := catalog.Type(row.productType)
			if !typ.IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, catalog.InvalidTypeMessage(typ)))
				result.Skipped++
				failedLines[row.lineNum] = struct{}{}
			}
		}
	}

	// 3. For each group, validate and write in a transaction
	for slug, rows := range groups {
		if skipSlugGroupSiblings(slug, rows, failedLines, result) {
			continue
		}

		groupType, conflict, seenTypes := slugGroupType(rows)
		if conflict {
			seenMsg := strings.Join(seenTypes, ", ")
			for _, row := range rows {
				result.Errors = append(result.Errors, fmt.Sprintf(
					"line %d: conflicting type values for slug %q: %s (empty cells count as simple when any type is set)",
					row.lineNum, slug, seenMsg,
				))
				result.Skipped++
				failedLines[row.lineNum] = struct{}{}
			}
			continue
		}

		// Check if product exists
		existing, err := imp.products.FindBySlug(ctx, slug)
		if err != nil {
			for _, row := range rows {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: find product: %v", row.lineNum, err))
				result.Skipped++
				failedLines[row.lineNum] = struct{}{}
			}
			continue
		}

		// Prepare product and variants
		var product *catalog.Product
		if existing != nil {
			stored := existing.Type
			if stored == "" {
				stored = catalog.TypeSimple
			}
			if groupType != "" && catalog.Type(groupType) != stored {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: type updates via CSV are not supported (product %q has type %q, CSV has %q)",
						row.lineNum, slug, stored, groupType,
					))
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
			}
			product = existing
		} else {
			p, err := catalog.NewProduct(id.New(), rows[0].name, slug)
			if err != nil {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: new product: %v", row.lineNum, err))
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
			}
			p.Description = rows[0].desc
			if groupType == "" {
				p.Type = catalog.TypeSimple
			} else {
				p.Type = catalog.Type(groupType)
			}
			if err := p.Validate(); err != nil {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", row.lineNum, err))
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
			}
			product = &p
		}

		// Prepare variants
		type preparedVariant struct {
			variant catalog.Variant
			lineNum int
		}
		var pvs []preparedVariant
		skip := false
		for _, row := range rows {
			v, err := catalog.NewVariant(id.New(), product.ID, row.sku)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: new variant: %v", row.lineNum, err))
				result.Skipped++
				skip = true
				break
			}
			v.Name = row.variantName
			attrs, attrErrs := imp.parseAndValidateAttrs(row.rawAttrs, row.lineNum)
			if len(attrErrs) > 0 {
				for _, e := range attrErrs {
					result.Errors = append(result.Errors, e)
				}
				result.Skipped++
				continue
			}
			if len(attrs) > 0 {
				v.Attributes = attrs
			}
			pvs = append(pvs, preparedVariant{variant: v, lineNum: row.lineNum})
		}
		if skip || len(pvs) == 0 {
			continue
		}

		// Build variant slice for persistence.
		variants := make([]catalog.Variant, len(pvs))
		for i, pv := range pvs {
			variants[i] = pv.variant
		}

		// 4. Write in transaction (when txStarter available), else direct writes
		if existing == nil && imp.txStarter != nil {
			tx, err := imp.txStarter.BeginTx(ctx, nil)
			if err != nil {
				for _, pv := range pvs {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: begin tx: %v", pv.lineNum, err))
					result.Skipped++
				}
				continue
			}
			txProducts, ok1 := imp.products.(txProductRepo)
			txVariants, ok2 := imp.variants.(txVariantRepo)
			if !ok1 || !ok2 {
				if rbErr := tx.Rollback(); rbErr != nil {
					return result, fmt.Errorf("import: repos do not support WithTx (products=%t, variants=%t); rollback: %w", ok1, ok2, rbErr)
				}
				return result, fmt.Errorf("import: txStarter provided but repos do not support WithTx (products=%t, variants=%t)", ok1, ok2)
			}
			prodRepo := txProducts.WithTx(tx)
			varRepo := txVariants.WithTx(tx)
			// Write product
			if err := prodRepo.Create(ctx, product); err != nil {
				tx.Rollback()
				for _, pv := range pvs {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create product: %v", pv.lineNum, err))
					result.Skipped++
				}
				continue
			}
			// Write variants
			allOk := true
			for _, v := range variants {
				if err := varRepo.Create(ctx, &v); err != nil {
					tx.Rollback()
					for _, pv := range pvs {
						result.Errors = append(result.Errors, fmt.Sprintf("line %d: create variant (rollback slug %q): %v", pv.lineNum, slug, err))
						result.Skipped++
					}
					allOk = false
					break
				}
			}
			if !allOk {
				continue
			}
			if err := tx.Commit(); err != nil {
				for _, pv := range pvs {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: commit: %v", pv.lineNum, err))
					result.Skipped++
				}
				continue
			}
			result.Products++
			result.Variants += len(variants)
		} else if existing == nil {
			// No txStarter: direct writes (tests / non-transactional mode)
			if err := imp.products.Create(ctx, product); err != nil {
				for _, pv := range pvs {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create product: %v", pv.lineNum, err))
					result.Skipped++
				}
				continue
			}
			result.Products++
			for i, v := range variants {
				if err := imp.variants.Create(ctx, &v); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create variant: %v", pvs[i].lineNum, err))
					result.Skipped++
					continue
				}
				result.Variants++
			}
		} else if imp.txStarter != nil {
			// Existing product + txStarter: add variants in a transaction
			tx, err := imp.txStarter.BeginTx(ctx, nil)
			if err != nil {
				for _, pv := range pvs {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: begin tx: %v", pv.lineNum, err))
					result.Skipped++
				}
				continue
			}
			txVB, ok := imp.variants.(txVariantRepo)
			if !ok {
				if rbErr := tx.Rollback(); rbErr != nil {
					return result, fmt.Errorf("import: variant repo does not support WithTx; rollback: %w", rbErr)
				}
				return result, fmt.Errorf("import: txStarter provided but variant repo does not support WithTx")
			}
			varRepo := txVB.WithTx(tx)
			allOk := true
			for _, v := range variants {
				if err := varRepo.Create(ctx, &v); err != nil {
					tx.Rollback()
					for _, pv := range pvs {
						result.Errors = append(result.Errors, fmt.Sprintf("line %d: create variant (rollback slug %q): %v", pv.lineNum, slug, err))
						result.Skipped++
					}
					allOk = false
					break
				}
			}
			if !allOk {
				continue
			}
			if err := tx.Commit(); err != nil {
				for _, pv := range pvs {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: commit: %v", pv.lineNum, err))
					result.Skipped++
				}
				continue
			}
			result.Variants += len(variants)
		} else {
			// Existing product, no txStarter: direct writes
			for i, v := range variants {
				if err := imp.variants.Create(ctx, &v); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: create variant: %v", pvs[i].lineNum, err))
					result.Skipped++
					continue
				}
				result.Variants++
			}
		}
	}

	return result, nil
}

// productImportRow is one CSV data row after header mapping.
type productImportRow struct {
	lineNum     int
	name        string
	slug        string
	sku         string
	desc        string
	productType string
	variantName string
	rawAttrs    map[string]string
}

// skipSlugGroupSiblings marks remaining rows in a slug group when at least one
// row already failed validation, so Skipped/Errors reflect every dropped line.
// Returns true when the group must be skipped.
func skipSlugGroupSiblings(slug string, rows []productImportRow, failedLines map[int]struct{}, result *Result) bool {
	anyFailed := false
	for _, row := range rows {
		if _, ok := failedLines[row.lineNum]; ok {
			anyFailed = true
			break
		}
	}
	if !anyFailed {
		return false
	}
	for _, row := range rows {
		if _, ok := failedLines[row.lineNum]; ok {
			continue
		}
		result.Errors = append(result.Errors, fmt.Sprintf(
			"line %d: skipped because another row for slug %q failed validation",
			row.lineNum, slug,
		))
		result.Skipped++
		failedLines[row.lineNum] = struct{}{}
	}
	return true
}

// slugGroupType returns the agreed type for a slug group.
// All-empty → ("", false, nil) so create defaults to simple and existing products
// can gain variants without a type column. When any cell is non-empty, blanks are
// normalized to simple for consensus; more than one distinct value → conflict.
func slugGroupType(rows []productImportRow) (groupType string, conflict bool, seenSorted []string) {
	anyNonEmpty := false
	for _, row := range rows {
		if row.productType != "" {
			anyNonEmpty = true
			break
		}
	}
	if !anyNonEmpty {
		return "", false, nil
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		t := row.productType
		if t == "" {
			t = string(catalog.TypeSimple)
		}
		seen[t] = struct{}{}
	}
	seenSorted = make([]string, 0, len(seen))
	for t := range seen {
		seenSorted = append(seenSorted, t)
	}
	sort.Strings(seenSorted)
	if len(seen) == 1 {
		return seenSorted[0], false, seenSorted
	}
	return "", true, seenSorted
}

// colVal returns the trimmed value for a column name, or "" if absent.
func colVal(record []string, colIndex map[string]int, col string) string {
	idx, ok := colIndex[col]
	if !ok || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

// parseAndValidateAttrs converts raw CSV attribute values to typed values using
// the registry (when available) and validates them against the group (when set).
func (imp *ProductImporter) parseAndValidateAttrs(raw map[string]string, lineNum int) (map[string]interface{}, []string) {
	if len(raw) == 0 && (imp.registry == nil || imp.groupCode == "") {
		return nil, nil
	}
	parsed := make(map[string]interface{}, len(raw))
	var errs []string
	for code, val := range raw {
		if imp.registry != nil {
			if attr, ok := imp.registry.Attribute(code); ok {
				v, err := parseAttributeValue(val, attr)
				if err != nil {
					errs = append(errs, fmt.Sprintf("line %d: %v", lineNum, err))
					continue
				}
				if err := attr.Validate(v); err != nil {
					errs = append(errs, fmt.Sprintf("line %d: %v", lineNum, err))
					continue
				}
				parsed[code] = v
				continue
			}
		}
		parsed[code] = val
	}
	if len(errs) > 0 {
		return nil, errs
	}
	if imp.registry != nil && imp.groupCode != "" {
		for _, e := range imp.registry.ValidateAttributes(imp.groupCode, parsed) {
			errs = append(errs, fmt.Sprintf("line %d: %v", lineNum, e))
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return parsed, nil
}

// parseAttributeValue converts a raw CSV string to a typed value per the
// attribute definition.
func parseAttributeValue(raw string, attr catalog.Attribute) (interface{}, error) {
	switch attr.Type {
	case catalog.AttributeTypeText, catalog.AttributeTypeSelect:
		return raw, nil
	case catalog.AttributeTypeNumber:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %q is not a valid number", attr.Code, raw)
		}
		return v, nil
	case catalog.AttributeTypeBoolean:
		switch strings.ToLower(raw) {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no":
			return false, nil
		default:
			return nil, fmt.Errorf("attribute %q: %q is not a valid boolean", attr.Code, raw)
		}
	}
	return raw, nil
}
