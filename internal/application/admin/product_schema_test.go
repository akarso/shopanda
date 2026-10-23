package admin_test

import (
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
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
	if len(form.Fields) != 5 {
		t.Fatalf("fields count = %d, want 5", len(form.Fields))
	}

	// Verify field names in order.
	wantNames := []string{"name", "slug", "description", "type", "status"}
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

	typeField := form.Fields[3]
	if typeField.Type != "select" {
		t.Errorf("type field type = %q, want %q", typeField.Type, "select")
	}
	if !typeField.Required {
		t.Error("type field should be required")
	}
	wantTypes := catalog.AllTypes()
	if len(typeField.Options) != len(wantTypes) {
		t.Fatalf("type options count = %d, want %d", len(typeField.Options), len(wantTypes))
	}
	for i, want := range wantTypes {
		if typeField.Options[i].Value != string(want) {
			t.Errorf("type option[%d].Value = %q, want %q", i, typeField.Options[i].Value, want)
		}
		if typeField.Options[i].Label != adminApp.ProductTypeLabel(want) {
			t.Errorf("type option[%d].Label = %q, want %q", i, typeField.Options[i].Label, adminApp.ProductTypeLabel(want))
		}
	}
	if typeField.Default != string(catalog.TypeSimple) {
		t.Errorf("type default = %v, want %q", typeField.Default, catalog.TypeSimple)
	}

	// status field has options.
	statusField := form.Fields[4]
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

	// Each field declares its scope so the admin UI can render scope badges.
	wantScopes := map[string]string{
		"name":        "translatable",
		"slug":        "global",
		"description": "translatable",
		"type":        "global",
		"status":      "global",
	}
	for _, field := range form.Fields {
		want := wantScopes[field.Name]
		got, _ := field.Meta["scope"].(string)
		if got != want {
			t.Errorf("field %q scope = %q, want %q", field.Name, got, want)
		}
	}

	if len(form.Sections) != 0 {
		t.Errorf("Sections = %d, want 0 until later tracks register type-specific panels", len(form.Sections))
	}
}

func TestProductTypeLabel_UnknownFallsBackToRaw(t *testing.T) {
	if got := adminApp.ProductTypeLabel(catalog.Type("kit")); got != "kit" {
		t.Errorf("ProductTypeLabel(kit) = %q, want kit", got)
	}
}

func TestRegisterProductFormSection_ValidatesTypes(t *testing.T) {
	r := admin.NewRegistry()
	adminApp.RegisterProductSchemas(r)

	err := adminApp.RegisterProductFormSection(r, admin.FormSection{
		ID: "bundle-components", Title: "Bundle", Types: []string{"bundles"},
	})
	if err == nil {
		t.Fatal("expected invalid type error")
	}

	err = adminApp.RegisterProductFormSection(r, admin.FormSection{
		ID: "bundle-components", Title: "Bundle", Types: []string{"bundle"},
	})
	if err != nil {
		t.Fatalf("RegisterProductFormSection: %v", err)
	}
	f, _ := r.Form("product.form")
	if len(f.Sections) != 1 || f.Sections[0].Types[0] != "bundle" {
		t.Fatalf("sections = %+v", f.Sections)
	}
}

func TestRegisterProductFormSection_EmptyTypesAlwaysVisible(t *testing.T) {
	r := admin.NewRegistry()
	adminApp.RegisterProductSchemas(r)
	err := adminApp.RegisterProductFormSection(r, admin.FormSection{
		ID: "notes", Title: "Notes", Types: nil,
	})
	if err != nil {
		t.Fatalf("RegisterProductFormSection: %v", err)
	}
	f, _ := r.Form("product.form")
	if len(f.Sections[0].Types) != 0 {
		t.Errorf("Types = %v, want empty (always visible)", f.Sections[0].Types)
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
	if len(grid.Columns) != 7 {
		t.Fatalf("columns count = %d, want 7", len(grid.Columns))
	}

	wantCols := []string{"id", "name", "slug", "type", "status", "created_at", "updated_at"}
	for i, want := range wantCols {
		if grid.Columns[i].Name != want {
			t.Errorf("column[%d].Name = %q, want %q", i, grid.Columns[i].Name, want)
		}
	}
}
