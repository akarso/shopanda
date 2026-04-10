package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/id"
)

// CategoryResult holds the summary of a category import run.
type CategoryResult struct {
	Created int
	Updated int
	Skipped int
	Errors  []string
}

// CategoryImporter imports categories from CSV.
type CategoryImporter struct {
	categories catalog.CategoryRepository
}

// NewCategoryImporter creates a CategoryImporter.
func NewCategoryImporter(categories catalog.CategoryRepository) *CategoryImporter {
	return &CategoryImporter{categories: categories}
}

type catRow struct {
	lineNum    int
	name       string
	slug       string
	parentSlug string
	position   int
}

// Import reads CSV rows from r and persists categories.
//
// Required columns: name, slug.
// Optional columns: parent_slug, position.
//
// parent_slug references another category by slug (from the same file or
// already existing in the database). Categories are inserted in topological
// order so that parents are created before their children.
// When a slug already exists in the database the row updates the category
// instead of creating a new one.
func (imp *CategoryImporter) Import(ctx context.Context, r io.Reader) (*CategoryResult, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("category import: read header: %w", err)
	}

	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	nameIdx, hasName := colIdx["name"]
	slugIdx, hasSlug := colIdx["slug"]
	if !hasName || !hasSlug {
		return nil, fmt.Errorf("category import: CSV must have 'name' and 'slug' columns")
	}

	parentSlugIdx, hasParentSlug := colIdx["parent_slug"]
	positionIdx, hasPosition := colIdx["position"]

	// Parse all rows.
	var rows []catRow
	lineNum := 1 // header is line 1

	var parseErrors []string

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("line %d: %v", lineNum, err))
			continue
		}

		name := ""
		if nameIdx < len(record) {
			name = strings.TrimSpace(record[nameIdx])
		}
		slug := ""
		if slugIdx < len(record) {
			slug = strings.TrimSpace(record[slugIdx])
		}

		parentSlug := ""
		if hasParentSlug && parentSlugIdx < len(record) {
			parentSlug = strings.TrimSpace(record[parentSlugIdx])
		}

		position := 0
		if hasPosition && positionIdx < len(record) {
			posStr := strings.TrimSpace(record[positionIdx])
			if posStr != "" {
				if p, pErr := strconv.Atoi(posStr); pErr == nil {
					position = p
				}
			}
		}

		rows = append(rows, catRow{
			lineNum:    lineNum,
			name:       name,
			slug:       slug,
			parentSlug: parentSlug,
			position:   position,
		})
	}

	result := &CategoryResult{}
	result.Errors = append(result.Errors, parseErrors...)
	result.Skipped += len(parseErrors)

	// Validate required fields and collect valid rows.
	var validRows []catRow
	for _, row := range rows {
		if row.name == "" || row.slug == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: name and slug are required", row.lineNum))
			result.Skipped++
			continue
		}
		validRows = append(validRows, row)
	}

	// Deduplicate: last row wins for the same slug.
	bySlug := make(map[string]catRow)
	var order []string
	for _, row := range validRows {
		if _, exists := bySlug[row.slug]; !exists {
			order = append(order, row.slug)
		}
		bySlug[row.slug] = row
	}

	// Topological sort: parents before children.
	sorted, err := catTopoSort(order, bySlug)
	if err != nil {
		return nil, fmt.Errorf("category import: %w", err)
	}

	// Resolve parent slugs and upsert.
	slugToID := make(map[string]string)

	for _, slug := range sorted {
		row := bySlug[slug]

		// Resolve parent.
		var parentID *string
		if row.parentSlug != "" {
			if pid, ok := slugToID[row.parentSlug]; ok {
				parentID = &pid
			} else {
				parent, fErr := imp.categories.FindBySlug(ctx, row.parentSlug)
				if fErr != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: find parent %q: %v", row.lineNum, row.parentSlug, fErr))
					result.Skipped++
					continue
				}
				if parent == nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d: parent slug %q not found", row.lineNum, row.parentSlug))
					result.Skipped++
					continue
				}
				pid := parent.ID
				parentID = &pid
				slugToID[row.parentSlug] = parent.ID
			}
		}

		// Check if category already exists.
		existing, fErr := imp.categories.FindBySlug(ctx, slug)
		if fErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: find category: %v", row.lineNum, fErr))
			result.Skipped++
			continue
		}

		if existing != nil {
			existing.Name = row.name
			existing.ParentID = parentID
			existing.Position = row.position
			if uErr := imp.categories.Update(ctx, existing); uErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: update category: %v", row.lineNum, uErr))
				result.Skipped++
				continue
			}
			slugToID[slug] = existing.ID
			result.Updated++
		} else {
			cat, cErr := catalog.NewCategory(id.New(), row.name, slug)
			if cErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: new category: %v", row.lineNum, cErr))
				result.Skipped++
				continue
			}
			cat.ParentID = parentID
			cat.Position = row.position
			if crErr := imp.categories.Create(ctx, &cat); crErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d: create category: %v", row.lineNum, crErr))
				result.Skipped++
				continue
			}
			slugToID[slug] = cat.ID
			result.Created++
		}
	}

	return result, nil
}

// catTopoSort returns slugs in topological order (parents before children).
// Only in-file parent references participate in the ordering; external parents
// are resolved at insert time.
func catTopoSort(order []string, bySlug map[string]catRow) ([]string, error) {
	inDegree := make(map[string]int, len(order))
	children := make(map[string][]string)

	for _, slug := range order {
		inDegree[slug] = 0
	}

	for _, slug := range order {
		row := bySlug[slug]
		if row.parentSlug != "" {
			if _, inFile := bySlug[row.parentSlug]; inFile {
				inDegree[slug]++
				children[row.parentSlug] = append(children[row.parentSlug], slug)
			}
		}
	}

	// Kahn's algorithm.
	var queue []string
	for _, slug := range order {
		if inDegree[slug] == 0 {
			queue = append(queue, slug)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		slug := queue[0]
		queue = queue[1:]
		sorted = append(sorted, slug)

		for _, child := range children[slug] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if len(sorted) != len(order) {
		return nil, fmt.Errorf("cycle detected in category parent references")
	}

	return sorted, nil
}
