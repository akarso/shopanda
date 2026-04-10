package importer_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// mockCategoryRepo implements catalog.CategoryRepository for testing.
type mockCategoryRepo struct {
	categories map[string]*catalog.Category // slug → category
	createErr  error
	updateErr  error
	findErr    error
}

func newMockCategoryRepo() *mockCategoryRepo {
	return &mockCategoryRepo{
		categories: make(map[string]*catalog.Category),
	}
}

func (m *mockCategoryRepo) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, c := range m.categories {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindBySlug(_ context.Context, slug string) (*catalog.Category, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if c, ok := m.categories[slug]; ok {
		return c, nil
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindByParentID(_ context.Context, parentID *string) ([]catalog.Category, error) {
	return nil, nil
}

func (m *mockCategoryRepo) FindAll(_ context.Context) ([]catalog.Category, error) {
	return nil, nil
}

func (m *mockCategoryRepo) Create(_ context.Context, c *catalog.Category) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.categories[c.Slug] = c
	return nil
}

func (m *mockCategoryRepo) Update(_ context.Context, c *catalog.Category) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	c.UpdatedAt = time.Now().UTC()
	m.categories[c.Slug] = c
	return nil
}

func TestCategoryImport_Basic(t *testing.T) {
	csv := "name,slug,parent_slug,position\nElectronics,electronics,,0\nPhones,phones,electronics,1\nSmartphones,smartphones,phones,0\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 3 {
		t.Errorf("Created = %d, want 3", result.Created)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}

	// Verify parent-child relationships.
	phones := repo.categories["phones"]
	if phones == nil {
		t.Fatal("phones category not found")
	}
	electronics := repo.categories["electronics"]
	if electronics == nil {
		t.Fatal("electronics category not found")
	}
	if phones.ParentID == nil || *phones.ParentID != electronics.ID {
		t.Errorf("phones.ParentID = %v, want %s", phones.ParentID, electronics.ID)
	}

	smartphones := repo.categories["smartphones"]
	if smartphones == nil {
		t.Fatal("smartphones category not found")
	}
	if smartphones.ParentID == nil || *smartphones.ParentID != phones.ID {
		t.Errorf("smartphones.ParentID = %v, want %s", smartphones.ParentID, phones.ID)
	}
}

func TestCategoryImport_MissingNameColumn(t *testing.T) {
	csv := "slug,parent_slug\nelectronics,\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing name column")
	}
	if !strings.Contains(err.Error(), "'name' and 'slug'") {
		t.Errorf("error = %q, want mention of name and slug", err.Error())
	}
}

func TestCategoryImport_MissingSlugColumn(t *testing.T) {
	csv := "name,parent_slug\nElectronics,\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing slug column")
	}
}

func TestCategoryImport_EmptyName(t *testing.T) {
	csv := "name,slug\n,electronics\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Errorf("Errors count = %d, want 1", len(result.Errors))
	}
}

func TestCategoryImport_EmptySlug(t *testing.T) {
	csv := "name,slug\nElectronics,\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestCategoryImport_ParentNotFound(t *testing.T) {
	csv := "name,slug,parent_slug\nPhones,phones,nonexistent\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "not found") {
		t.Errorf("error = %q, want mention of not found", result.Errors[0])
	}
}

func TestCategoryImport_Update(t *testing.T) {
	now := time.Now().UTC()
	existing := catalog.NewCategoryFromDB("cat-1", nil, "Old Name", "electronics", 0, nil, now, now)

	repo := newMockCategoryRepo()
	repo.categories["electronics"] = existing

	csv := "name,slug,position\nNew Name,electronics,5\n"
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Updated != 1 {
		t.Errorf("Updated = %d, want 1", result.Updated)
	}
	if result.Created != 0 {
		t.Errorf("Created = %d, want 0", result.Created)
	}

	updated := repo.categories["electronics"]
	if updated.Name != "New Name" {
		t.Errorf("Name = %q, want New Name", updated.Name)
	}
	if updated.Position != 5 {
		t.Errorf("Position = %d, want 5", updated.Position)
	}
}

func TestCategoryImport_CycleDetection(t *testing.T) {
	csv := "name,slug,parent_slug\nA,a,c\nB,b,a\nC,c,b\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want mention of cycle", err.Error())
	}
}

func TestCategoryImport_DuplicateSlug(t *testing.T) {
	csv := "name,slug\nFirst,electronics\nSecond,electronics\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	// Last row wins, only one category created.
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}

	cat := repo.categories["electronics"]
	if cat == nil {
		t.Fatal("electronics not found")
	}
	if cat.Name != "Second" {
		t.Errorf("Name = %q, want Second (last row wins)", cat.Name)
	}
}

func TestCategoryImport_CreateError(t *testing.T) {
	csv := "name,slug\nElectronics,electronics\n"
	repo := newMockCategoryRepo()
	repo.createErr = errTest
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestCategoryImport_ExternalParent(t *testing.T) {
	// Parent exists in DB, not in CSV.
	now := time.Now().UTC()
	parent := catalog.NewCategoryFromDB("parent-1", nil, "Electronics", "electronics", 0, nil, now, now)

	repo := newMockCategoryRepo()
	repo.categories["electronics"] = parent

	csv := "name,slug,parent_slug\nPhones,phones,electronics\n"
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}

	phones := repo.categories["phones"]
	if phones == nil {
		t.Fatal("phones not found")
	}
	if phones.ParentID == nil || *phones.ParentID != "parent-1" {
		t.Errorf("phones.ParentID = %v, want parent-1", phones.ParentID)
	}
}

func TestCategoryImport_CSVParseError(t *testing.T) {
	// Malformed row: wrong number of fields with quote error.
	input := "name,slug\nElectronics,electronics\n\"broken\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(result.Errors))
	}
	if !strings.Contains(result.Errors[0], "line 3") {
		t.Errorf("error = %q, want mention of line 3", result.Errors[0])
	}
}

func TestCategoryImport_InvalidPosition(t *testing.T) {
	input := "name,slug,position\nElectronics,electronics,abc\n"
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 0 {
		t.Errorf("Created = %d, want 0", result.Created)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(result.Errors))
	}
	if !strings.Contains(result.Errors[0], "invalid position") {
		t.Errorf("error = %q, want mention of invalid position", result.Errors[0])
	}
}

func TestCategoryImport_ReparentCycle(t *testing.T) {
	// Set up existing tree: root → child.
	now := time.Now().UTC()
	rootID := "root-1"
	root := catalog.NewCategoryFromDB("root-1", nil, "Root", "root", 0, nil, now, now)
	child := catalog.NewCategoryFromDB("child-1", &rootID, "Child", "child", 0, nil, now, now)

	repo := newMockCategoryRepo()
	repo.categories["root"] = root
	repo.categories["child"] = child

	// Try to reparent root under child → cycle.
	input := "name,slug,parent_slug\nRoot,root,child\n"
	imp := importer.NewCategoryImporter(repo)

	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Updated != 0 {
		t.Errorf("Updated = %d, want 0 (cycle should be rejected)", result.Updated)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(result.Errors))
	}
	if !strings.Contains(result.Errors[0], "cycle") {
		t.Errorf("error = %q, want mention of cycle", result.Errors[0])
	}
}

func TestCategoryImport_FailedParentCascade(t *testing.T) {
	// Parent create fails → child should be skipped with "failed earlier" error.
	repo := newMockCategoryRepo()
	repo.createErr = fmt.Errorf("db down")
	imp := importer.NewCategoryImporter(repo)

	input := "name,slug,parent_slug,position\nParent,parent,,0\nChild,child,parent,0\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (parent + child)", result.Skipped)
	}
	if result.Created != 0 {
		t.Errorf("Created = %d, want 0", result.Created)
	}
	// Child error should mention "failed earlier".
	foundCascade := false
	for _, e := range result.Errors {
		if strings.Contains(e, "failed earlier") {
			foundCascade = true
		}
	}
	if !foundCascade {
		t.Errorf("expected cascade error mentioning 'failed earlier', got %v", result.Errors)
	}
}

func TestCategoryImport_FieldCountMismatch(t *testing.T) {
	// Row with fewer fields than header should still be processed (trailing
	// optional columns get defaults). csv.ErrFieldCount should be non-fatal.
	repo := newMockCategoryRepo()
	imp := importer.NewCategoryImporter(repo)

	// Header has 4 columns; data row has only 2 (missing parent_slug and position).
	input := "name,slug,parent_slug,position\nElectronics,electronics\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}
	cat := repo.categories["electronics"]
	if cat == nil {
		t.Fatal("electronics category not found")
	}
	if cat.ParentID != nil {
		t.Errorf("ParentID = %v, want nil (missing column defaults to empty)", cat.ParentID)
	}
	if cat.Position != 0 {
		t.Errorf("Position = %d, want 0 (missing column defaults to 0)", cat.Position)
	}
}

var errTest = fmt.Errorf("test error")
