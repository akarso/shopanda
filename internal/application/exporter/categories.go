package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// CategoryResult holds the summary of a category export run.
type CategoryResult struct {
	Entries int
	Orphans int
}

// CategoryExporter writes categories to CSV.
type CategoryExporter struct {
	categories catalog.CategoryRepository
}

// NewCategoryExporter creates a CategoryExporter.
func NewCategoryExporter(categories catalog.CategoryRepository) *CategoryExporter {
	return &CategoryExporter{categories: categories}
}

// Export writes all categories to w in CSV format.
//
// CSV columns: name, slug, parent_slug, position.
// Categories are written in tree order (parents before children).
func (exp *CategoryExporter) Export(ctx context.Context, w io.Writer) (*CategoryResult, error) {
	all, err := exp.categories.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("category export: find all: %w", err)
	}

	// Build ID→slug map for parent resolution.
	idToSlug := make(map[string]string, len(all))
	for _, c := range all {
		idToSlug[c.ID] = c.Slug
	}

	// Build tree order (parents before children).
	sorted, orphans := catTreeOrder(all)

	writer := csv.NewWriter(w)

	if err := writer.Write([]string{"name", "slug", "parent_slug", "position"}); err != nil {
		return nil, fmt.Errorf("category export: write header: %w", err)
	}

	result := &CategoryResult{}

	for _, c := range sorted {
		parentSlug := ""
		if c.ParentID != nil {
			if s, ok := idToSlug[*c.ParentID]; ok {
				parentSlug = s
			}
		}

		row := []string{
			sanitizeCSVCell(c.Name),
			sanitizeCSVCell(c.Slug),
			sanitizeCSVCell(parentSlug),
			strconv.Itoa(c.Position),
		}
		if err := writer.Write(row); err != nil {
			return nil, fmt.Errorf("category export: write row: %w", err)
		}
		result.Entries++
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("category export: flush csv: %w", err)
	}

	result.Orphans = orphans

	return result, nil
}

// catTreeOrder returns categories sorted so parents come before children.
// Within the same parent, categories keep their original ordering
// (position asc, name asc as returned by FindAll).
// The second return value is the number of orphan categories whose ParentID
// references a non-existent parent; these are appended at the end.
func catTreeOrder(all []catalog.Category) ([]catalog.Category, int) {
	children := make(map[string][]int) // parentID → indices
	var rootIndices []int

	for i, c := range all {
		if c.ParentID == nil {
			rootIndices = append(rootIndices, i)
		} else {
			children[*c.ParentID] = append(children[*c.ParentID], i)
		}
	}

	result := make([]catalog.Category, 0, len(all))
	visited := make([]bool, len(all))
	var walk func(indices []int)
	walk = func(indices []int) {
		for _, i := range indices {
			result = append(result, all[i])
			visited[i] = true
			walk(children[all[i].ID])
		}
	}
	walk(rootIndices)

	// Append orphans (categories whose parent_id references a non-existent ID).
	orphans := 0
	for i, v := range visited {
		if !v {
			result = append(result, all[i])
			orphans++
		}
	}

	return result, orphans
}
