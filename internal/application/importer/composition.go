package importer

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// CompositionResult is the summary of an additive composition import.
// Omitted rows are not deletes.
type CompositionResult struct {
	Added   int
	Skipped int
	Errors  []string
}

// BundleComponentImporter adds bundle component links from CSV.
// Columns: parent_slug, component_slug, quantity, required.
// required empty means true. Existing links are upserted via BundleService.Add.
type BundleComponentImporter struct {
	products catalog.ProductRepository
	bundles  *catalogApp.BundleService
}

// NewBundleComponentImporter creates a bundle-component CSV importer.
// A nil product repository or bundle service panics so a missed wire fails at startup.
func NewBundleComponentImporter(products catalog.ProductRepository, bundles *catalogApp.BundleService) *BundleComponentImporter {
	if products == nil {
		panic("importer: bundle component importer: product repository must not be nil")
	}
	if bundles == nil {
		panic("importer: bundle component importer: bundle service must not be nil")
	}
	return &BundleComponentImporter{products: products, bundles: bundles}
}

// Import resolves slugs with FindBySlug and upserts each row. It does not
// delete links that are absent from the file.
func (imp *BundleComponentImporter) Import(ctx context.Context, r io.Reader) (*CompositionResult, error) {
	if imp.products == nil || imp.bundles == nil {
		return nil, fmt.Errorf("bundle component import: product repository and bundle service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("bundle component import: read header: %w", err)
	}
	colIdx := compositionColIndex(header)
	for _, col := range []string{"parent_slug", "component_slug", "quantity", "required"} {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("bundle component import: missing required column %q", col)
		}
	}

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
		parentSlug := colValRow(row, "parent_slug")
		componentSlug := colValRow(row, "component_slug")
		if parentSlug == "" || componentSlug == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: parent_slug and component_slug are required", lineNum))
			result.Skipped++
			continue
		}
		qtyRaw := colValRow(row, "quantity")
		qty, err := strconv.Atoi(qtyRaw)
		if err != nil || qty < 1 {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: quantity must be an integer >= 1", lineNum))
			result.Skipped++
			continue
		}
		required, err := parseRequiredCell(colValRow(row, "required"))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}
		parent, err := cachedProductBySlug(ctx, imp.products, cache, parentSlug)
		if err != nil {
			return nil, fmt.Errorf("bundle component import: find parent slug %q: %w", parentSlug, err)
		}
		if parent == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown parent slug %q", lineNum, parentSlug))
			result.Skipped++
			continue
		}
		component, err := cachedProductBySlug(ctx, imp.products, cache, componentSlug)
		if err != nil {
			return nil, fmt.Errorf("bundle component import: find component slug %q: %w", componentSlug, err)
		}
		if component == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown component slug %q", lineNum, componentSlug))
			result.Skipped++
			continue
		}
		link := &catalog.BundleComponent{
			BundleProductID:    parent.ID,
			ComponentProductID: component.ID,
			Quantity:           qty,
			Required:           required,
		}
		if err := imp.bundles.Add(ctx, link); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, compositionErrText(err)))
			result.Skipped++
			continue
		}
		result.Added++
	}
	return result, nil
}

// GroupedMemberImporter adds grouped membership links from CSV.
// Columns: parent_slug, member_slug. Duplicate links are insert-ignore.
type GroupedMemberImporter struct {
	products catalog.ProductRepository
	grouped  *catalogApp.GroupedService
}

// NewGroupedMemberImporter creates a grouped-member CSV importer.
// A nil product repository or grouped service panics so a missed wire fails at startup.
func NewGroupedMemberImporter(products catalog.ProductRepository, grouped *catalogApp.GroupedService) *GroupedMemberImporter {
	if products == nil {
		panic("importer: grouped member importer: product repository must not be nil")
	}
	if grouped == nil {
		panic("importer: grouped member importer: grouped service must not be nil")
	}
	return &GroupedMemberImporter{products: products, grouped: grouped}
}

// Import resolves slugs with FindBySlug and inserts each row. It does not
// delete members that are absent from the file.
func (imp *GroupedMemberImporter) Import(ctx context.Context, r io.Reader) (*CompositionResult, error) {
	if imp.products == nil || imp.grouped == nil {
		return nil, fmt.Errorf("grouped member import: product repository and grouped service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("grouped member import: read header: %w", err)
	}
	colIdx := compositionColIndex(header)
	for _, col := range []string{"parent_slug", "member_slug"} {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("grouped member import: missing required column %q", col)
		}
	}

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
		parentSlug := colValRow(row, "parent_slug")
		memberSlug := colValRow(row, "member_slug")
		if parentSlug == "" || memberSlug == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: parent_slug and member_slug are required", lineNum))
			result.Skipped++
			continue
		}
		parent, err := cachedProductBySlug(ctx, imp.products, cache, parentSlug)
		if err != nil {
			return nil, fmt.Errorf("grouped member import: find parent slug %q: %w", parentSlug, err)
		}
		if parent == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown parent slug %q", lineNum, parentSlug))
			result.Skipped++
			continue
		}
		member, err := cachedProductBySlug(ctx, imp.products, cache, memberSlug)
		if err != nil {
			return nil, fmt.Errorf("grouped member import: find member slug %q: %w", memberSlug, err)
		}
		if member == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown member slug %q", lineNum, memberSlug))
			result.Skipped++
			continue
		}
		link := &catalog.GroupedMember{
			GroupProductID:  parent.ID,
			MemberProductID: member.ID,
		}
		if err := imp.grouped.Add(ctx, link); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, compositionErrText(err)))
			result.Skipped++
			continue
		}
		result.Added++
	}
	return result, nil
}

func compositionColIndex(header []string) map[string]int {
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		name := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(h, "\uFEFF")))
		colIdx[name] = i
	}
	return colIdx
}

func parseRequiredCell(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid required %q", raw)
	}
}

func cachedProductBySlug(ctx context.Context, products catalog.ProductRepository, cache map[string]*catalog.Product, slug string) (*catalog.Product, error) {
	if p, ok := cache[slug]; ok {
		return p, nil
	}
	p, err := products.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	cache[slug] = p
	return p, nil
}

func compositionErrText(err error) string {
	var appErr *apperror.Error
	if errors.As(err, &appErr) && appErr.Message != "" {
		return appErr.Message
	}
	return err.Error()
}
