package catalog_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestNewCollection(t *testing.T) {
	before := time.Now().UTC()
	c, err := catalog.NewCollection("col-1", "Summer Sale", "summer-sale", catalog.CollectionManual)
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("NewCollection() error = %v", err)
	}
	if c.ID != "col-1" {
		t.Errorf("ID = %q, want col-1", c.ID)
	}
	if c.Name != "Summer Sale" {
		t.Errorf("Name = %q, want Summer Sale", c.Name)
	}
	if c.Slug != "summer-sale" {
		t.Errorf("Slug = %q, want summer-sale", c.Slug)
	}
	if c.Type != catalog.CollectionManual {
		t.Errorf("Type = %q, want manual", c.Type)
	}
	if c.Rules == nil {
		t.Error("Rules should be initialised")
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

func TestNewCollection_Dynamic(t *testing.T) {
	c, err := catalog.NewCollection("col-2", "Under 50", "under-50", catalog.CollectionDynamic)
	if err != nil {
		t.Fatalf("NewCollection() error = %v", err)
	}
	if c.Type != catalog.CollectionDynamic {
		t.Errorf("Type = %q, want dynamic", c.Type)
	}
}

func TestNewCollection_Validation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		cName    string
		slug     string
		collType catalog.CollectionType
	}{
		{"empty id", "", "Name", "slug", catalog.CollectionManual},
		{"empty name", "id-1", "", "slug", catalog.CollectionManual},
		{"empty slug", "id-1", "Name", "", catalog.CollectionManual},
		{"whitespace id", "  ", "Name", "slug", catalog.CollectionManual},
		{"whitespace name", "id-1", "  ", "slug", catalog.CollectionManual},
		{"whitespace slug", "id-1", "Name", "  ", catalog.CollectionManual},
		{"invalid type", "id-1", "Name", "slug", catalog.CollectionType("invalid")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := catalog.NewCollection(tc.id, tc.cName, tc.slug, tc.collType)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCollectionType_IsValid(t *testing.T) {
	if !catalog.CollectionManual.IsValid() {
		t.Error("manual should be valid")
	}
	if !catalog.CollectionDynamic.IsValid() {
		t.Error("dynamic should be valid")
	}
	if catalog.CollectionType("other").IsValid() {
		t.Error("other should not be valid")
	}
}

func TestCollection_IsManual(t *testing.T) {
	c, err := catalog.NewCollection("id-1", "Sale", "sale", catalog.CollectionManual)
	if err != nil {
		t.Fatalf("NewCollection() error = %v", err)
	}
	if !c.IsManual() {
		t.Error("expected IsManual() = true")
	}
	if c.IsDynamic() {
		t.Error("expected IsDynamic() = false")
	}
}

func TestCollection_IsDynamic(t *testing.T) {
	c, err := catalog.NewCollection("id-1", "Budget", "budget", catalog.CollectionDynamic)
	if err != nil {
		t.Fatalf("NewCollection() error = %v", err)
	}
	if !c.IsDynamic() {
		t.Error("expected IsDynamic() = true")
	}
	if c.IsManual() {
		t.Error("expected IsManual() = false")
	}
}

func TestNewCollectionFromDB(t *testing.T) {
	now := time.Now().UTC()
	rules := map[string]interface{}{"price": map[string]interface{}{"lt": float64(50)}}
	meta := map[string]interface{}{"title": "SEO Title"}

	c := catalog.NewCollectionFromDB("col-1", "Under 50", "under-50", catalog.CollectionDynamic, rules, meta, now, now)

	if c.ID != "col-1" {
		t.Errorf("ID = %q, want col-1", c.ID)
	}
	if c.Name != "Under 50" {
		t.Errorf("Name = %q, want Under 50", c.Name)
	}
	if c.Type != catalog.CollectionDynamic {
		t.Errorf("Type = %q, want dynamic", c.Type)
	}
	if c.Rules["price"] == nil {
		t.Error("Rules[price] should be set")
	}
	if c.Meta["title"] != "SEO Title" {
		t.Errorf("Meta[title] = %v, want SEO Title", c.Meta["title"])
	}
}

func TestNewCollectionFromDB_NilMaps(t *testing.T) {
	c := catalog.NewCollectionFromDB("col-1", "Test", "test", catalog.CollectionManual, nil, nil, time.Now(), time.Now())
	if c.Rules == nil {
		t.Error("Rules should be initialised even when nil is passed")
	}
	if c.Meta == nil {
		t.Error("Meta should be initialised even when nil is passed")
	}
}
