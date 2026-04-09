package admin_test

import (
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/admin"
)

func TestRegisterProductSchemas_Form(t *testing.T) {
	r := admin.NewRegistry()
	adminApp.RegisterProductSchemas(r)

	form, ok := r.Form("product.form")
	if !ok {
		t.Fatal("product.form not registered")
	}
	if form.Name != "product.form" {
		t.Errorf("form.Name = %q, want %q", form.Name, "product.form")
	}
	if len(form.Fields) != 4 {
		t.Fatalf("fields count = %d, want 4", len(form.Fields))
	}

	// Verify field names in order.
	wantNames := []string{"name", "slug", "description", "status"}
	for i, want := range wantNames {
		if form.Fields[i].Name != want {
			t.Errorf("field[%d].Name = %q, want %q", i, form.Fields[i].Name, want)
		}
	}

	// name and slug required.
	if !form.Fields[0].Required {
		t.Error("name field should be required")
	}
	if !form.Fields[1].Required {
		t.Error("slug field should be required")
	}
	if form.Fields[2].Required {
		t.Error("description field should not be required")
	}

	// status field has options.
	statusField := form.Fields[3]
	if statusField.Type != "select" {
		t.Errorf("status field type = %q, want %q", statusField.Type, "select")
	}
	if len(statusField.Options) != 3 {
		t.Fatalf("status options count = %d, want 3", len(statusField.Options))
	}
	wantValues := []string{"draft", "active", "archived"}
	for i, want := range wantValues {
		if statusField.Options[i].Value != want {
			t.Errorf("option[%d].Value = %q, want %q", i, statusField.Options[i].Value, want)
		}
	}
	if statusField.Default != "draft" {
		t.Errorf("status default = %v, want %q", statusField.Default, "draft")
	}
}

func TestRegisterProductSchemas_Grid(t *testing.T) {
	r := admin.NewRegistry()
	adminApp.RegisterProductSchemas(r)

	grid, ok := r.Grid("product.grid")
	if !ok {
		t.Fatal("product.grid not registered")
	}
	if grid.Name != "product.grid" {
		t.Errorf("grid.Name = %q, want %q", grid.Name, "product.grid")
	}
	if len(grid.Columns) != 6 {
		t.Fatalf("columns count = %d, want 6", len(grid.Columns))
	}

	wantCols := []string{"id", "name", "slug", "status", "created_at", "updated_at"}
	for i, want := range wantCols {
		if grid.Columns[i].Name != want {
			t.Errorf("column[%d].Name = %q, want %q", i, grid.Columns[i].Name, want)
		}
	}
}
