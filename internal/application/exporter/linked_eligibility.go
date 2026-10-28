package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// LinkedEligibleParentExporter writes explicit parent allowlist rows as CSV.
// Columns: child_slug, parent_slug.
type LinkedEligibleParentExporter struct {
	rules *catalogApp.LinkedEligibilityService
}

// NewLinkedEligibleParentExporter creates a linked-eligible-parent CSV exporter.
func NewLinkedEligibleParentExporter(products catalog.ProductRepository, rules *catalogApp.LinkedEligibilityService) *LinkedEligibleParentExporter {
	_ = products // retained for call-site compatibility with composition exporters
	if rules == nil {
		panic("exporter: linked eligible parent exporter: eligibility service must not be nil")
	}
	return &LinkedEligibleParentExporter{rules: rules}
}

// Export writes allowlist rows from a single joined query (no product-catalog paging).
func (exp *LinkedEligibleParentExporter) Export(ctx context.Context, w io.Writer) (*CompositionResult, error) {
	if exp.rules == nil {
		return nil, fmt.Errorf("linked eligible parent export: eligibility service is required")
	}
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"child_slug", "parent_slug"}); err != nil {
		return nil, fmt.Errorf("linked eligible parent export: write header: %w", err)
	}
	rows, err := exp.rules.ListAllParentSlugRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("linked eligible parent export: list: %w", err)
	}
	result := &CompositionResult{}
	for _, row := range rows {
		if row.ChildSlug == "" || row.ParentSlug == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("skip row with empty slug (child=%q parent=%q)", row.ChildSlug, row.ParentSlug))
			result.Skipped++
			continue
		}
		if err := writer.Write([]string{row.ChildSlug, row.ParentSlug}); err != nil {
			return nil, fmt.Errorf("linked eligible parent export: write row: %w", err)
		}
		result.Rows++
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("linked eligible parent export: flush csv: %w", err)
	}
	return result, nil
}

// LinkedEligibleCategoryExporter writes category eligibility rules as CSV.
// Columns: child_slug, category_slug.
type LinkedEligibleCategoryExporter struct {
	rules *catalogApp.LinkedEligibilityService
}

// NewLinkedEligibleCategoryExporter creates a linked-eligible-category CSV exporter.
func NewLinkedEligibleCategoryExporter(
	products catalog.ProductRepository,
	categories catalog.CategoryRepository,
	rules *catalogApp.LinkedEligibilityService,
) *LinkedEligibleCategoryExporter {
	_, _ = products, categories // retained for call-site compatibility
	if rules == nil {
		panic("exporter: linked eligible category exporter: eligibility service must not be nil")
	}
	return &LinkedEligibleCategoryExporter{rules: rules}
}

// Export writes category rules from a single joined query (no product-catalog paging).
func (exp *LinkedEligibleCategoryExporter) Export(ctx context.Context, w io.Writer) (*CompositionResult, error) {
	if exp.rules == nil {
		return nil, fmt.Errorf("linked eligible category export: eligibility service is required")
	}
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"child_slug", "category_slug"}); err != nil {
		return nil, fmt.Errorf("linked eligible category export: write header: %w", err)
	}
	rows, err := exp.rules.ListAllCategorySlugRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("linked eligible category export: list: %w", err)
	}
	result := &CompositionResult{}
	for _, row := range rows {
		if row.ChildSlug == "" || row.CategorySlug == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("skip row with empty slug (child=%q category=%q)", row.ChildSlug, row.CategorySlug))
			result.Skipped++
			continue
		}
		if err := writer.Write([]string{row.ChildSlug, row.CategorySlug}); err != nil {
			return nil, fmt.Errorf("linked eligible category export: write row: %w", err)
		}
		result.Rows++
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("linked eligible category export: flush csv: %w", err)
	}
	return result, nil
}
