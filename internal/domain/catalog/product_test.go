package catalog_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestNewProduct(t *testing.T) {
	before := time.Now().UTC()
	p, err := catalog.NewProduct("prod-1", "T-Shirt", "t-shirt")
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("NewProduct() error = %v", err)
	}

	if p.ID != "prod-1" {
		t.Errorf("ID = %q, want prod-1", p.ID)
	}
	if p.Name != "T-Shirt" {
		t.Errorf("Name = %q, want T-Shirt", p.Name)
	}
	if p.Slug != "t-shirt" {
		t.Errorf("Slug = %q, want t-shirt", p.Slug)
	}
	if p.Status != catalog.StatusDraft {
		t.Errorf("Status = %q, want draft", p.Status)
	}
	if p.Attributes == nil {
		t.Error("Attributes should be initialised")
	}
	if p.CreatedAt.Before(before) || p.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", p.CreatedAt, before, after)
	}
	if p.UpdatedAt.Before(before) || p.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v, want between %v and %v", p.UpdatedAt, before, after)
	}
}

func TestStatus_IsValid(t *testing.T) {
	tests := []struct {
		status catalog.Status
		want   bool
	}{
		{catalog.StatusDraft, true},
		{catalog.StatusActive, true},
		{catalog.StatusArchived, true},
		{catalog.Status("deleted"), false},
		{catalog.Status(""), false},
	}
	for _, tc := range tests {
		name := string(tc.status)
		if name == "" {
			name = "(empty)"
		}
		t.Run(name, func(t *testing.T) {
			if got := tc.status.IsValid(); got != tc.want {
				t.Errorf("Status(%q).IsValid() = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestNewProduct_DefaultDescription(t *testing.T) {
	p, err := catalog.NewProduct("id-1", "Shoes", "shoes")
	if err != nil {
		t.Fatalf("NewProduct() error = %v", err)
	}
	if p.Description != "" {
		t.Errorf("Description = %q, want empty", p.Description)
	}
}

func TestNewProduct_Validation(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		pName string
		slug  string
	}{
		{"empty id", "", "Shoes", "shoes"},
		{"empty name", "id-1", "", "shoes"},
		{"empty slug", "id-1", "Shoes", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := catalog.NewProduct(tc.id, tc.pName, tc.slug)
			if err == nil {
				t.Error("expected error for empty field")
			}
		})
	}
}

func TestEventConstants(t *testing.T) {
	if catalog.EventProductCreated != "catalog.product.created" {
		t.Errorf("EventProductCreated = %q", catalog.EventProductCreated)
	}
	if catalog.EventProductUpdated != "catalog.product.updated" {
		t.Errorf("EventProductUpdated = %q", catalog.EventProductUpdated)
	}
}
