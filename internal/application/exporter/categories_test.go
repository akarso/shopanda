package exporter_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// --- mock ---

type mockCategoryRepo struct {
	categories []catalog.Category
	findAllErr error
}

func (m *mockCategoryRepo) FindByID(_ context.Context, _ string) (*catalog.Category, error) {
	return nil, nil
}
func (m *mockCategoryRepo) FindBySlug(_ context.Context, _ string) (*catalog.Category, error) {
	return nil, nil
}
func (m *mockCategoryRepo) FindByParentID(_ context.Context, _ *string) ([]catalog.Category, error) {
	return nil, nil
}
func (m *mockCategoryRepo) FindAll(_ context.Context) ([]catalog.Category, error) {
	if m.findAllErr != nil {
		return nil, m.findAllErr
	}
	return m.categories, nil
}
func (m *mockCategoryRepo) Create(_ context.Context, _ *catalog.Category) error { return nil }
func (m *mockCategoryRepo) Update(_ context.Context, _ *catalog.Category) error { return nil }

// --- tests ---

func TestCategoryExport_Basic(t *testing.T) {
	now := time.Now().UTC()
	parentID := "cat-1"
	repo := &mockCategoryRepo{
		categories: []catalog.Category{
			{ID: "cat-1", Name: "Electronics", Slug: "electronics", Position: 0, Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now},
			{ID: "cat-2", ParentID: &parentID, Name: "Phones", Slug: "phones", Position: 1, Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now},
		},
	}

	exp := exporter.NewCategoryExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Entries != 2 {
		t.Errorf("Entries = %d, want 2", result.Entries)
	}

	records := parseCSV(t, &buf)
	// header + 2 rows
	if len(records) != 3 {
		t.Fatalf("rows = %d, want 3", len(records))
	}
	if records[0][0] != "name" || records[0][1] != "slug" || records[0][2] != "parent_slug" || records[0][3] != "position" {
		t.Errorf("header = %v, unexpected", records[0])
	}
	// First row: Electronics (root).
	if records[1][1] != "electronics" || records[1][2] != "" {
		t.Errorf("row 1 = %v, want electronics with empty parent", records[1])
	}
	// Second row: Phones (child of electronics).
	if records[2][1] != "phones" || records[2][2] != "electronics" {
		t.Errorf("row 2 = %v, want phones with parent electronics", records[2])
	}
}

func TestCategoryExport_Empty(t *testing.T) {
	repo := &mockCategoryRepo{}
	exp := exporter.NewCategoryExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Entries != 0 {
		t.Errorf("Entries = %d, want 0", result.Entries)
	}

	records := parseCSV(t, &buf)
	if len(records) != 1 {
		t.Fatalf("rows = %d, want 1 (header only)", len(records))
	}
}

func TestCategoryExport_TreeOrder(t *testing.T) {
	now := time.Now().UTC()
	rootID := "cat-1"
	childID := "cat-2"

	// Provide categories in flat order (child of child first to test reordering).
	repo := &mockCategoryRepo{
		categories: []catalog.Category{
			{ID: "cat-1", Name: "Root", Slug: "root", Position: 0, Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now},
			{ID: "cat-2", ParentID: &rootID, Name: "Child", Slug: "child", Position: 0, Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now},
			{ID: "cat-3", ParentID: &childID, Name: "Grandchild", Slug: "grandchild", Position: 0, Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now},
		},
	}

	exp := exporter.NewCategoryExporter(repo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Entries != 3 {
		t.Errorf("Entries = %d, want 3", result.Entries)
	}

	records := parseCSV(t, &buf)
	// Verify tree order: root → child → grandchild.
	if records[1][1] != "root" {
		t.Errorf("row 1 slug = %q, want root", records[1][1])
	}
	if records[2][1] != "child" {
		t.Errorf("row 2 slug = %q, want child", records[2][1])
	}
	if records[3][1] != "grandchild" {
		t.Errorf("row 3 slug = %q, want grandchild", records[3][1])
	}
}

func TestCategoryExport_FindAllError(t *testing.T) {
	repo := &mockCategoryRepo{findAllErr: fmt.Errorf("db down")}
	exp := exporter.NewCategoryExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCategoryExport_FormulaInjection(t *testing.T) {
	now := time.Now().UTC()
	repo := &mockCategoryRepo{
		categories: []catalog.Category{
			{ID: "cat-1", Name: "=cmd()", Slug: "+evil", Position: 0, Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now},
		},
	}

	exp := exporter.NewCategoryExporter(repo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	records := parseCSV(t, &buf)
	if records[1][0] != "'=cmd()" {
		t.Errorf("name = %q, want '=cmd() (sanitized)", records[1][0])
	}
	if records[1][1] != "'+evil" {
		t.Errorf("slug = %q, want '+evil (sanitized)", records[1][1])
	}
}
