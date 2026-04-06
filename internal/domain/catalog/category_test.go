package catalog_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestNewCategory(t *testing.T) {
	before := time.Now().UTC()
	c, err := catalog.NewCategory("cat-1", "Electronics", "electronics")
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("NewCategory() error = %v", err)
	}
	if c.ID != "cat-1" {
		t.Errorf("ID = %q, want cat-1", c.ID)
	}
	if c.Name != "Electronics" {
		t.Errorf("Name = %q, want Electronics", c.Name)
	}
	if c.Slug != "electronics" {
		t.Errorf("Slug = %q, want electronics", c.Slug)
	}
	if c.ParentID != nil {
		t.Errorf("ParentID = %v, want nil", c.ParentID)
	}
	if c.Position != 0 {
		t.Errorf("Position = %d, want 0", c.Position)
	}
	if c.Meta == nil {
		t.Error("Meta should be initialised")
	}
	if c.CreatedAt.Before(before) || c.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", c.CreatedAt, before, after)
	}
	if c.UpdatedAt.Before(before) || c.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v, want between %v and %v", c.UpdatedAt, before, after)
	}
}

func TestNewCategory_Validation(t *testing.T) {
	tests := []struct {
		name string
		id   string
		cName string
		slug string
	}{
		{"empty id", "", "Name", "slug"},
		{"empty name", "id-1", "", "slug"},
		{"empty slug", "id-1", "Name", ""},
		{"whitespace id", "  ", "Name", "slug"},
		{"whitespace name", "id-1", "  ", "slug"},
		{"whitespace slug", "id-1", "Name", "  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := catalog.NewCategory(tc.id, tc.cName, tc.slug)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCategory_IsRoot(t *testing.T) {
	c, err := catalog.NewCategory("id-1", "Root", "root")
	if err != nil {
		t.Fatalf("NewCategory() error = %v", err)
	}
	if !c.IsRoot() {
		t.Error("expected IsRoot() = true for nil ParentID")
	}

	parentID := "parent-1"
	c.ParentID = &parentID
	if c.IsRoot() {
		t.Error("expected IsRoot() = false when ParentID is set")
	}
}

func TestNewCategoryFromDB(t *testing.T) {
	parentID := "parent-1"
	now := time.Now().UTC()
	meta := map[string]interface{}{"title": "SEO Title"}

	c := catalog.NewCategoryFromDB("cat-1", &parentID, "Phones", "phones", 5, meta, now, now)

	if c.ID != "cat-1" {
		t.Errorf("ID = %q, want cat-1", c.ID)
	}
	if c.ParentID == nil || *c.ParentID != "parent-1" {
		t.Errorf("ParentID = %v, want parent-1", c.ParentID)
	}
	if c.Name != "Phones" {
		t.Errorf("Name = %q, want Phones", c.Name)
	}
	if c.Slug != "phones" {
		t.Errorf("Slug = %q, want phones", c.Slug)
	}
	if c.Position != 5 {
		t.Errorf("Position = %d, want 5", c.Position)
	}
	if c.Meta["title"] != "SEO Title" {
		t.Errorf("Meta[title] = %v, want SEO Title", c.Meta["title"])
	}
}

func TestNewCategoryFromDB_NilMeta(t *testing.T) {
	c := catalog.NewCategoryFromDB("cat-1", nil, "Root", "root", 0, nil, time.Now(), time.Now())
	if c.Meta == nil {
		t.Error("Meta should be initialised even when nil is passed")
	}
}
