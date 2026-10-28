package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// CompositionResult is the summary of a composition export.
type CompositionResult struct {
	Rows    int
	Skipped int
	Errors  []string
}

// BundleComponentExporter writes bundle component links as CSV.
// Columns: parent_slug, component_slug, quantity, required.
type BundleComponentExporter struct {
	products catalog.ProductRepository
	bundles  *catalogApp.BundleService
}

// NewBundleComponentExporter creates a bundle-component CSV exporter.
// A nil product repository or bundle service panics so a missed wire fails at startup.
func NewBundleComponentExporter(products catalog.ProductRepository, bundles *catalogApp.BundleService) *BundleComponentExporter {
	if products == nil {
		panic("exporter: bundle component exporter: product repository must not be nil")
	}
	if bundles == nil {
		panic("exporter: bundle component exporter: bundle service must not be nil")
	}
	return &BundleComponentExporter{products: products, bundles: bundles}
}

// Export lists bundle products under operator scope and writes one row per link.
func (exp *BundleComponentExporter) Export(ctx context.Context, w io.Writer) (*CompositionResult, error) {
	if exp.products == nil || exp.bundles == nil {
		return nil, fmt.Errorf("bundle component export: product repository and bundle service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"parent_slug", "component_slug", "quantity", "required"}); err != nil {
		return nil, fmt.Errorf("bundle component export: write header: %w", err)
	}
	result := &CompositionResult{}
	lineNum := 1
	if err := eachProduct(ctx, exp.products, catalog.TypeBundle, func(p catalog.Product) error {
		rows, err := exp.bundles.List(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("bundle component export: list %q: %w", p.Slug, err)
		}
		children, childErrs := lookupCompositionChildren(ctx, exp.products, componentProductIDs(rows))
		for _, row := range rows {
			lineNum++
			if err := childErrs[row.ComponentProductID]; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: component product %q of bundle %q: %v", lineNum, row.ComponentProductID, p.Slug, err))
				result.Skipped++
				continue
			}
			child := children[row.ComponentProductID]
			if child == nil || child.Slug == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: component product %q of bundle %q has no slug", lineNum, row.ComponentProductID, p.Slug))
				result.Skipped++
				continue
			}
			required := "false"
			if row.Required {
				required = "true"
			}
			if err := writer.Write([]string{p.Slug, child.Slug, strconv.Itoa(row.Quantity), required}); err != nil {
				return fmt.Errorf("bundle component export: write row: %w", err)
			}
			result.Rows++
		}
		return nil
	}); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("bundle component export: flush csv: %w", err)
	}
	return result, nil
}

// GroupedMemberExporter writes grouped membership links as CSV.
// Columns: parent_slug, member_slug.
type GroupedMemberExporter struct {
	products catalog.ProductRepository
	grouped  *catalogApp.GroupedService
}

// NewGroupedMemberExporter creates a grouped-member CSV exporter.
// A nil product repository or grouped service panics so a missed wire fails at startup.
func NewGroupedMemberExporter(products catalog.ProductRepository, grouped *catalogApp.GroupedService) *GroupedMemberExporter {
	if products == nil {
		panic("exporter: grouped member exporter: product repository must not be nil")
	}
	if grouped == nil {
		panic("exporter: grouped member exporter: grouped service must not be nil")
	}
	return &GroupedMemberExporter{products: products, grouped: grouped}
}

// Export lists grouped products under operator scope and writes one row per member.
func (exp *GroupedMemberExporter) Export(ctx context.Context, w io.Writer) (*CompositionResult, error) {
	if exp.products == nil || exp.grouped == nil {
		return nil, fmt.Errorf("grouped member export: product repository and grouped service are required")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"parent_slug", "member_slug"}); err != nil {
		return nil, fmt.Errorf("grouped member export: write header: %w", err)
	}
	result := &CompositionResult{}
	lineNum := 1
	if err := eachProduct(ctx, exp.products, catalog.TypeGrouped, func(p catalog.Product) error {
		rows, err := exp.grouped.List(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("grouped member export: list %q: %w", p.Slug, err)
		}
		children, childErrs := lookupCompositionChildren(ctx, exp.products, memberProductIDs(rows))
		for _, row := range rows {
			lineNum++
			if err := childErrs[row.MemberProductID]; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: member product %q of group %q: %v", lineNum, row.MemberProductID, p.Slug, err))
				result.Skipped++
				continue
			}
			child := children[row.MemberProductID]
			if child == nil || child.Slug == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: member product %q of group %q has no slug", lineNum, row.MemberProductID, p.Slug))
				result.Skipped++
				continue
			}
			if err := writer.Write([]string{p.Slug, child.Slug}); err != nil {
				return fmt.Errorf("grouped member export: write row: %w", err)
			}
			result.Rows++
		}
		return nil
	}); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("grouped member export: flush csv: %w", err)
	}
	return result, nil
}

type compositionProductLister interface {
	ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error)
}

func componentProductIDs(rows []catalog.BundleComponent) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ComponentProductID
	}
	return ids
}

func memberProductIDs(rows []catalog.GroupedMember) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.MemberProductID
	}
	return ids
}

// lookupCompositionChildren loads child products in one batch when the
// repository implements ListByIDs, otherwise one FindByID per id.
// A lookup error is recorded for that id and does not abort the export.
func lookupCompositionChildren(ctx context.Context, products catalog.ProductRepository, ids []string) (map[string]*catalog.Product, map[string]error) {
	found := map[string]*catalog.Product{}
	failed := map[string]error{}
	unique := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return found, failed
	}
	if lister, ok := products.(compositionProductLister); ok {
		byID, err := lister.ListByIDs(ctx, unique)
		if err != nil {
			for _, id := range unique {
				failed[id] = err
			}
			return found, failed
		}
		if byID == nil {
			return found, failed
		}
		return byID, failed
	}
	for _, id := range unique {
		child, err := products.FindByID(ctx, id)
		if err != nil {
			failed[id] = err
			continue
		}
		if child != nil {
			found[id] = child
		}
	}
	return found, failed
}

func eachProduct(ctx context.Context, products catalog.ProductRepository, productType catalog.Type, fn func(catalog.Product) error) error {
	offset := 0
	for {
		// Empty productType lists every type (linked eligibility export).
		page, err := products.List(ctx, catalog.ListFilter{Type: productType, Offset: offset, Limit: pageSize})
		if err != nil {
			return fmt.Errorf("composition export: list products: %w", err)
		}
		if len(page) == 0 {
			return nil
		}
		for _, p := range page {
			if err := fn(p); err != nil {
				return err
			}
		}
		if len(page) < pageSize {
			return nil
		}
		offset += len(page)
	}
}
