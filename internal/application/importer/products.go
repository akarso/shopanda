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
	"time"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// productReindexTriggerTimeout bounds the post-import ReindexService.Trigger
// call so it does not inherit a canceled operation context (same as stock).
const productReindexTriggerTimeout = 30 * time.Second

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
	bus       *event.Bus
	reindex   *searchApp.ReindexService
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

// WithBus publishes catalog.EventProductCreated after each successful product
// create (Publish-then-PublishAsync, matching composition admin). Nil skips
// publish. Search IndexUpdateSubscriber listens OnAsync for this event.
func (imp *ProductImporter) WithBus(bus *event.Bus) *ProductImporter {
	imp.bus = bus
	return imp
}

// WithReindex batch-triggers one scoped reindex for every product this import
// created (CLI fallback when the short-lived process has no
// IndexUpdateSubscriber). Prefer WithBus so serve/worker subscribers can
// react; WithReindex covers import:products when wired in cmd/api.
func (imp *ProductImporter) WithReindex(reindex *searchApp.ReindexService) *ProductImporter {
	imp.reindex = reindex
	return imp
}

// requiredColumns lists the CSV headers that must be present.
var requiredColumns = []string{"name", "slug", "sku"}

// knownColumns are CSV headers handled by the core import logic.
// Any other header is treated as an attribute column.
//
// "type" is reserved for product taxonomy (PR-1052). pricing_mode, stock_mode,
// explode_order_lines are reserved for bundle mode columns (PR-1066).
// requires_linked_parent is a product flag (PR-1068/PR-1073). Empty cells on
// create are false. A non-empty consensus that differs from the stored value
// on an existing product is an error — CSV does not update the flag.
// visibility columns are per-axis override modes (PR-1055/PR-1081): the four
// exact headers visibility_catalog, visibility_search, visibility_individually,
// visibility_purchasable. Empty cells on create are auto. A non-empty consensus
// that differs from the stored value on an existing product is an error — CSV
// does not update modes.
// "status" is reserved for product lifecycle (PR-1081). Empty cells on create
// are draft. A non-empty consensus that differs from the stored value on an
// existing product is an error — CSV does not update status.
// A CSV that previously used a free-form attribute column with one of these
// names must rename that attribute; values in the reserved columns are never
// stored in variant.Attributes.
var knownColumns = map[string]struct{}{
	"name": {}, "slug": {}, "sku": {}, "description": {}, "variant_name": {}, "type": {}, "status": {},
	"pricing_mode": {}, "stock_mode": {}, "explode_order_lines": {},
	"requires_linked_parent": {},
	"visibility_catalog":     {}, "visibility_search": {}, "visibility_individually": {}, "visibility_purchasable": {},
}

// Import reads CSV rows from r and persists them as products and variants.
//
// Expected CSV columns (order does not matter, header row required):
//
//	name         — product name (required)
//	slug         — product slug (required, used to group variants)
//	description  — product description (optional)
//	type         — product type (optional; empty → simple on create; case-sensitive like admin API)
//	status       — product lifecycle (optional; empty → draft on create; case-sensitive; create-only)
//	pricing_mode — fixed or dynamic (optional; empty → fixed on create; bundle-only dynamic)
//	stock_mode   — fixed or dynamic (optional; empty → fixed on create; bundle-only dynamic)
//	explode_order_lines — true or false (optional; empty → false on create; only when both modes are dynamic)
//	requires_linked_parent — true or false (optional; empty → false on create; create-only)
//	visibility_catalog|search|individually|purchasable — auto|visible|hidden (optional; empty → auto on create; create-only)
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
// Type and status updates on existing products are rejected (create-only).
// Mode columns use the same consensus: all-empty keeps create defaults
// (fixed / fixed / false / auto×4 / draft) and does not check an existing product. When any
// cell in a column is set, blanks count as that column's default. A non-empty
// consensus that differs from the stored value is an error and the slug is
// skipped — CSV does not update modes/status on existing products.
func (imp *ProductImporter) Import(ctx context.Context, r io.Reader) (*Result, error) {
	ctx = catalog.WithOperatorProductReadScope(ctx)
	createdProductIDs := map[string]struct{}{}
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
		explodeRaw, explodeOK := normalizeExplodeCell(colValRow(rowMap, "explode_order_lines"))
		linkedRaw, linkedOK := normalizeBoolCell(colValRow(rowMap, "requires_linked_parent"))
		row := productImportRow{
			lineNum:     lineNum,
			name:        colValRow(rowMap, "name"),
			slug:        colValRow(rowMap, "slug"),
			sku:         colValRow(rowMap, "sku"),
			desc:        colValRow(rowMap, "description"),
			productType: colValRow(rowMap, "type"),   // case-sensitive; matches admin API
			status:      colValRow(rowMap, "status"), // case-sensitive; matches admin API
			pricingMode: colValRow(rowMap, "pricing_mode"),
			stockMode:   colValRow(rowMap, "stock_mode"),
			explodeRaw:  explodeRaw,
			explodeOK:   explodeOK,
			linkedRaw:   linkedRaw,
			linkedOK:    linkedOK,
			visCatalog:  colValRow(rowMap, "visibility_catalog"),
			visSearch:   colValRow(rowMap, "visibility_search"),
			visIndiv:    colValRow(rowMap, "visibility_individually"),
			visPurch:    colValRow(rowMap, "visibility_purchasable"),
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
		rowFailed := false
		if row.productType != "" {
			typ := catalog.Type(row.productType)
			if !typ.IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, catalog.InvalidTypeMessage(typ)))
				rowFailed = true
			}
		}
		if row.status != "" {
			st := catalog.Status(row.status)
			if !st.IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, catalog.InvalidStatusMessage(st)))
				rowFailed = true
			}
		}
		if row.pricingMode != "" && !catalog.PricingMode(row.pricingMode).IsValid() {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, catalog.InvalidPricingModeMessage(catalog.PricingMode(row.pricingMode))))
			rowFailed = true
		}
		if row.stockMode != "" && !catalog.StockMode(row.stockMode).IsValid() {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, catalog.InvalidStockModeMessage(catalog.StockMode(row.stockMode))))
			rowFailed = true
		}
		if !row.explodeOK {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid explode_order_lines %q", row.lineNum, row.explodeRaw))
			rowFailed = true
		}
		if !row.linkedOK {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: invalid requires_linked_parent %q", row.lineNum, row.linkedRaw))
			rowFailed = true
		}
		for _, axis := range []struct {
			name, raw string
		}{
			{"visibility_catalog", row.visCatalog},
			{"visibility_search", row.visSearch},
			{"visibility_individually", row.visIndiv},
			{"visibility_purchasable", row.visPurch},
		} {
			if axis.raw != "" && !catalog.VisibilityMode(axis.raw).IsValid() {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s: %s", row.lineNum, axis.name, catalog.InvalidVisibilityModeMessage(catalog.VisibilityMode(axis.raw))))
				rowFailed = true
			}
		}
		if rowFailed {
			result.Skipped++
			failedLines[row.lineNum] = struct{}{}
		}
	}

	// 3. For each group, validate and write in a transaction
	for slug, rows := range groups {
		if skipSlugGroupSiblings(slug, rows, failedLines, result) {
			continue
		}

		groupType, conflict, seenTypes := slugGroupType(rows)
		groupStatus, statusConflict, seenStatuses := slugGroupConsensus(rows, func(row productImportRow) string { return row.status }, string(catalog.StatusDraft))
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
		if statusConflict {
			for _, row := range rows {
				result.Errors = append(result.Errors, fmt.Sprintf(
					"line %d: conflicting status values for slug %q: %s (empty cells count as draft when any status is set)",
					row.lineNum, slug, strings.Join(seenStatuses, ", "),
				))
				result.Skipped++
				failedLines[row.lineNum] = struct{}{}
			}
			continue
		}
		groupPricing, pricingConflict, pricingSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.pricingMode }, string(catalog.PricingModeFixed))
		groupStock, stockConflict, stockSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.stockMode }, string(catalog.StockModeFixed))
		groupExplode, explodeConflict, explodeSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.explodeRaw }, "false")
		groupLinked, linkedConflict, linkedSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.linkedRaw }, "false")
		autoMode := string(catalog.VisibilityModeAuto)
		groupVisCatalog, visCatalogConflict, visCatalogSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.visCatalog }, autoMode)
		groupVisSearch, visSearchConflict, visSearchSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.visSearch }, autoMode)
		groupVisIndiv, visIndivConflict, visIndivSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.visIndiv }, autoMode)
		groupVisPurch, visPurchConflict, visPurchSeen := slugGroupConsensus(rows, func(row productImportRow) string { return row.visPurch }, autoMode)
		visConflict := visCatalogConflict || visSearchConflict || visIndivConflict || visPurchConflict
		if pricingConflict || stockConflict || explodeConflict || linkedConflict || visConflict {
			for _, row := range rows {
				if pricingConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting pricing_mode values for slug %q: %s (empty cells count as fixed when any pricing_mode is set)",
						row.lineNum, slug, strings.Join(pricingSeen, ", "),
					))
				}
				if stockConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting stock_mode values for slug %q: %s (empty cells count as fixed when any stock_mode is set)",
						row.lineNum, slug, strings.Join(stockSeen, ", "),
					))
				}
				if explodeConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting explode_order_lines values for slug %q: %s (empty cells count as false when any explode_order_lines is set)",
						row.lineNum, slug, strings.Join(explodeSeen, ", "),
					))
				}
				if linkedConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting requires_linked_parent values for slug %q: %s (empty cells count as false when any requires_linked_parent is set)",
						row.lineNum, slug, strings.Join(linkedSeen, ", "),
					))
				}
				if visCatalogConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting visibility_catalog values for slug %q: %s (empty cells count as auto when any visibility_catalog is set)",
						row.lineNum, slug, strings.Join(visCatalogSeen, ", "),
					))
				}
				if visSearchConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting visibility_search values for slug %q: %s (empty cells count as auto when any visibility_search is set)",
						row.lineNum, slug, strings.Join(visSearchSeen, ", "),
					))
				}
				if visIndivConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting visibility_individually values for slug %q: %s (empty cells count as auto when any visibility_individually is set)",
						row.lineNum, slug, strings.Join(visIndivSeen, ", "),
					))
				}
				if visPurchConflict {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: conflicting visibility_purchasable values for slug %q: %s (empty cells count as auto when any visibility_purchasable is set)",
						row.lineNum, slug, strings.Join(visPurchSeen, ", "),
					))
				}
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
			storedStatus := existing.Status
			if storedStatus == "" {
				storedStatus = catalog.StatusDraft
			}
			if groupStatus != "" && catalog.Status(groupStatus) != storedStatus {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: status updates via CSV are not supported (product %q has status %q, CSV has %q)",
						row.lineNum, slug, storedStatus, groupStatus,
					))
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
			}
			if mismatch := existingModeMismatches(existing, groupPricing, groupStock, groupExplode); len(mismatch) > 0 {
				for _, row := range rows {
					for _, msg := range mismatch {
						result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, msg))
					}
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
			}
			if groupLinked != "" && existing.RequiresLinkedParent != (groupLinked == "true") {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf(
						"line %d: requires_linked_parent updates via CSV are not supported (product %q has requires_linked_parent %v, CSV has %q)",
						row.lineNum, slug, existing.RequiresLinkedParent, groupLinked,
					))
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
			}
			if mismatch := existingVisibilityMismatches(existing, groupVisCatalog, groupVisSearch, groupVisIndiv, groupVisPurch); len(mismatch) > 0 {
				for _, row := range rows {
					for _, msg := range mismatch {
						result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, msg))
					}
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
			if groupStatus == "" {
				p.Status = catalog.StatusDraft
			} else {
				p.Status = catalog.Status(groupStatus)
			}
			if err := applyImportModes(&p, groupPricing, groupStock, groupExplode); err != nil {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", row.lineNum, err))
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
			}
			if groupLinked != "" {
				p.RequiresLinkedParent = groupLinked == "true"
			}
			applyImportVisibilityModes(&p, groupVisCatalog, groupVisSearch, groupVisIndiv, groupVisPurch)
			// Same guard as admin create/activate: downloadable + active requires
			// at least one attached file. CSV create never attaches files, so
			// reject the combination — import as draft, then
			// import:downloadable-files, then activate via admin.
			if p.Type == catalog.TypeDownloadable && p.Status == catalog.StatusActive {
				for _, row := range rows {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", row.lineNum, catalog.DownloadableRequiresFileMessage))
					result.Skipped++
					failedLines[row.lineNum] = struct{}{}
				}
				continue
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
			createdProductIDs[product.ID] = struct{}{}
			imp.publishProductCreated(ctx, product)
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
			createdProductIDs[product.ID] = struct{}{}
			imp.publishProductCreated(ctx, product)
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

	if err := imp.triggerCreatedReindex(createdProductIDs); err != nil {
		return result, err
	}
	return result, nil
}

// publishProductCreated notifies search/cache/rewrite that a product was
// created. Publish-then-PublishAsync matches composition admin (PR-1080): a
// sync rewrite failure must not skip the async reindex path.
func (imp *ProductImporter) publishProductCreated(ctx context.Context, product *catalog.Product) {
	if imp.bus == nil || product == nil || product.ID == "" {
		return
	}
	evt := event.New(catalog.EventProductCreated, "product.importer", catalog.ProductCreatedData{
		ProductID: product.ID,
		Name:      product.Name,
		Slug:      product.Slug,
		Status:    product.Status,
	})
	if err := imp.bus.Publish(ctx, evt); err != nil {
		imp.bus.PublishAsync(evt)
	}
}

// triggerCreatedReindex fires one scoped reindex for products created in this
// run when WithReindex is wired (CLI import:products). Bus publish remains the
// preferred path for long-lived processes with IndexUpdateSubscriber.
func (imp *ProductImporter) triggerCreatedReindex(createdProductIDs map[string]struct{}) error {
	if imp.reindex == nil || len(createdProductIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(createdProductIDs))
	for id := range createdProductIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	ctx, cancel := context.WithTimeout(context.Background(), productReindexTriggerTimeout)
	defer cancel()
	if _, err := imp.reindex.Trigger(ctx, searchApp.ScopeProducts{IDs: ids}); err != nil {
		return fmt.Errorf("product import: reindex created products: %w", err)
	}
	return nil
}

// productImportRow is one CSV data row after header mapping.
type productImportRow struct {
	lineNum     int
	name        string
	slug        string
	sku         string
	desc        string
	productType string
	status      string
	pricingMode string
	stockMode   string
	explodeRaw  string
	explodeOK   bool
	linkedRaw   string
	linkedOK    bool
	visCatalog  string
	visSearch   string
	visIndiv    string
	visPurch    string
	variantName string
	rawAttrs    map[string]string
}

// normalizeExplodeCell accepts empty, true, and false (case-insensitive).
// An invalid token is returned unchanged with ok false so the row error can quote it.
func normalizeExplodeCell(raw string) (string, bool) {
	return normalizeBoolCell(raw)
}

// normalizeBoolCell accepts empty, true, and false (case-insensitive).
// An invalid token is returned unchanged with ok false so the row error can quote it.
func normalizeBoolCell(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	switch strings.ToLower(s) {
	case "true":
		return "true", true
	case "false":
		return "false", true
	default:
		return s, false
	}
}

func applyImportModes(p *catalog.Product, pricing, stock, explode string) error {
	if err := p.ApplyPricingMode(pricing, pricing != ""); err != nil {
		return err
	}
	if err := p.ApplyStockMode(stock, stock != ""); err != nil {
		return err
	}
	return p.ApplyExplodeOrderLines(explode == "true", explode != "")
}

func existingModeMismatches(existing *catalog.Product, pricing, stock, explode string) []string {
	var out []string
	storedPricing := existing.PricingMode
	if storedPricing == "" {
		storedPricing = catalog.PricingModeFixed
	}
	if pricing != "" && catalog.PricingMode(pricing) != storedPricing {
		out = append(out, fmt.Sprintf(
			"pricing_mode updates via CSV are not supported (product %q has pricing_mode %q, CSV has %q)",
			existing.Slug, storedPricing, pricing,
		))
	}
	storedStock := existing.StockMode
	if storedStock == "" {
		storedStock = catalog.StockModeFixed
	}
	if stock != "" && catalog.StockMode(stock) != storedStock {
		out = append(out, fmt.Sprintf(
			"stock_mode updates via CSV are not supported (product %q has stock_mode %q, CSV has %q)",
			existing.Slug, storedStock, stock,
		))
	}
	storedExplode := "false"
	if existing.ExplodeOrderLines {
		storedExplode = "true"
	}
	if explode != "" && explode != storedExplode {
		out = append(out, fmt.Sprintf(
			"explode_order_lines updates via CSV are not supported (product %q has explode_order_lines %q, CSV has %q)",
			existing.Slug, storedExplode, explode,
		))
	}
	return out
}

func applyImportVisibilityModes(p *catalog.Product, catalogMode, search, individually, purchasable string) {
	modes := p.VisibilityModes
	if !modes.Catalog.IsValid() || !modes.Search.IsValid() || !modes.Individually.IsValid() || !modes.Purchasable.IsValid() {
		modes = catalog.DefaultVisibilityAxes()
	}
	if catalogMode != "" {
		modes.Catalog = catalog.VisibilityMode(catalogMode)
	}
	if search != "" {
		modes.Search = catalog.VisibilityMode(search)
	}
	if individually != "" {
		modes.Individually = catalog.VisibilityMode(individually)
	}
	if purchasable != "" {
		modes.Purchasable = catalog.VisibilityMode(purchasable)
	}
	p.VisibilityModes = modes
}

func storedVisibilityMode(m catalog.VisibilityMode) catalog.VisibilityMode {
	if m == "" {
		return catalog.VisibilityModeAuto
	}
	return m
}

func existingVisibilityMismatches(existing *catalog.Product, catalogMode, search, individually, purchasable string) []string {
	var out []string
	stored := existing.VisibilityModes
	checks := []struct {
		col, csv string
		want     catalog.VisibilityMode
	}{
		{"visibility_catalog", catalogMode, storedVisibilityMode(stored.Catalog)},
		{"visibility_search", search, storedVisibilityMode(stored.Search)},
		{"visibility_individually", individually, storedVisibilityMode(stored.Individually)},
		{"visibility_purchasable", purchasable, storedVisibilityMode(stored.Purchasable)},
	}
	for _, c := range checks {
		if c.csv != "" && catalog.VisibilityMode(c.csv) != c.want {
			out = append(out, fmt.Sprintf(
				"%s updates via CSV are not supported (product %q has %s %q, CSV has %q)",
				c.col, existing.Slug, c.col, c.want, c.csv,
			))
		}
	}
	return out
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

// slugGroupConsensus returns the agreed value for one optional column.
// All-empty → ("", false, nil). When any cell is non-empty, blanks use
// blankDefault; more than one distinct value → conflict.
func slugGroupConsensus(rows []productImportRow, raw func(productImportRow) string, blankDefault string) (value string, conflict bool, seenSorted []string) {
	anyNonEmpty := false
	for _, row := range rows {
		if raw(row) != "" {
			anyNonEmpty = true
			break
		}
	}
	if !anyNonEmpty {
		return "", false, nil
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		v := raw(row)
		if v == "" {
			v = blankDefault
		}
		seen[v] = struct{}{}
	}
	seenSorted = make([]string, 0, len(seen))
	for v := range seen {
		seenSorted = append(seenSorted, v)
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
