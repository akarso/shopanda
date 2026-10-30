package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// LinkedEligibleParentImporter adds explicit parent allowlist rows from CSV.
// Columns: child_slug, parent_slug. Omitted rows are not deletes.
type LinkedEligibleParentImporter struct {
	products catalog.ProductRepository
	rules    *catalogApp.LinkedEligibilityService
}

// NewLinkedEligibleParentImporter creates a linked-eligible-parent CSV importer.
func NewLinkedEligibleParentImporter(products catalog.ProductRepository, rules *catalogApp.LinkedEligibilityService) *LinkedEligibleParentImporter {
	if products == nil {
		panic("importer: linked eligible parent importer: product repository must not be nil")
	}
	if rules == nil {
		panic("importer: linked eligible parent importer: eligibility service must not be nil")
	}
	return &LinkedEligibleParentImporter{products: products, rules: rules}
}

// Import resolves slugs and insert-ignores each allowlist row.
func (imp *LinkedEligibleParentImporter) Import(ctx context.Context, r io.Reader) (*CompositionResult, error) {
	if imp.products == nil || imp.rules == nil {
		return nil, fmt.Errorf("linked eligible parent import: product repository and eligibility service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("linked eligible parent import: read header: %w", err)
	}
	colIdx := compositionColIndex(header)
	for _, col := range []string{"child_slug", "parent_slug"} {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("linked eligible parent import: missing required column %q", col)
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
		childSlug := colValRow(row, "child_slug")
		parentSlug := colValRow(row, "parent_slug")
		if childSlug == "" || parentSlug == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: child_slug and parent_slug are required", lineNum))
			result.Skipped++
			continue
		}
		child, err := cachedProductBySlug(ctx, imp.products, cache, childSlug)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: find child slug %q: %v", lineNum, childSlug, err))
			result.Skipped++
			continue
		}
		if child == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown child slug %q", lineNum, childSlug))
			result.Skipped++
			continue
		}
		parent, err := cachedProductBySlug(ctx, imp.products, cache, parentSlug)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: find parent slug %q: %v", lineNum, parentSlug, err))
			result.Skipped++
			continue
		}
		if parent == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown parent slug %q", lineNum, parentSlug))
			result.Skipped++
			continue
		}
		link := &catalog.LinkedEligibleParent{
			ChildProductID:  child.ID,
			ParentProductID: parent.ID,
		}
		if err := imp.rules.AddParent(ctx, link); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, compositionErrText(err)))
			result.Skipped++
			continue
		}
		result.Added++
	}
	return result, nil
}

// LinkedEligibleCategoryImporter adds category eligibility rules from CSV.
// Columns: child_slug, category_slug. Omitted rows are not deletes.
type LinkedEligibleCategoryImporter struct {
	products   catalog.ProductRepository
	categories catalog.CategoryRepository
	rules      *catalogApp.LinkedEligibilityService
}

// NewLinkedEligibleCategoryImporter creates a linked-eligible-category CSV importer.
func NewLinkedEligibleCategoryImporter(
	products catalog.ProductRepository,
	categories catalog.CategoryRepository,
	rules *catalogApp.LinkedEligibilityService,
) *LinkedEligibleCategoryImporter {
	if products == nil {
		panic("importer: linked eligible category importer: product repository must not be nil")
	}
	if categories == nil {
		panic("importer: linked eligible category importer: category repository must not be nil")
	}
	if rules == nil {
		panic("importer: linked eligible category importer: eligibility service must not be nil")
	}
	return &LinkedEligibleCategoryImporter{products: products, categories: categories, rules: rules}
}

// Import resolves child and category slugs and insert-ignores each rule.
func (imp *LinkedEligibleCategoryImporter) Import(ctx context.Context, r io.Reader) (*CompositionResult, error) {
	if imp.products == nil || imp.categories == nil || imp.rules == nil {
		return nil, fmt.Errorf("linked eligible category import: product repository, category repository, and eligibility service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("linked eligible category import: read header: %w", err)
	}
	colIdx := compositionColIndex(header)
	for _, col := range []string{"child_slug", "category_slug"} {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("linked eligible category import: missing required column %q", col)
		}
	}

	result := &CompositionResult{}
	productCache := map[string]*catalog.Product{}
	categoryCache := map[string]*catalog.Category{}
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
		childSlug := colValRow(row, "child_slug")
		categorySlug := colValRow(row, "category_slug")
		if childSlug == "" || categorySlug == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: child_slug and category_slug are required", lineNum))
			result.Skipped++
			continue
		}
		child, err := cachedProductBySlug(ctx, imp.products, productCache, childSlug)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: find child slug %q: %v", lineNum, childSlug, err))
			result.Skipped++
			continue
		}
		if child == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown child slug %q", lineNum, childSlug))
			result.Skipped++
			continue
		}
		cat, err := cachedCategoryBySlug(ctx, imp.categories, categoryCache, categorySlug)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: find category slug %q: %v", lineNum, categorySlug, err))
			result.Skipped++
			continue
		}
		if cat == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: unknown category slug %q", lineNum, categorySlug))
			result.Skipped++
			continue
		}
		link := &catalog.LinkedEligibleCategory{
			ChildProductID: child.ID,
			CategoryID:     cat.ID,
		}
		if err := imp.rules.AddCategory(ctx, link); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %s", lineNum, compositionErrText(err)))
			result.Skipped++
			continue
		}
		result.Added++
	}
	return result, nil
}

func cachedCategoryBySlug(ctx context.Context, categories catalog.CategoryRepository, cache map[string]*catalog.Category, slug string) (*catalog.Category, error) {
	slug = strings.TrimSpace(slug)
	if c, ok := cache[slug]; ok {
		return c, nil
	}
	c, err := categories.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	cache[slug] = c
	return c, nil
}
