package admin_test

import (
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/admin"
)

func TestRegisterPageSchemas_Form(t *testing.T) {
	r := admin.NewRegistry()
	adminApp.RegisterPageSchemas(r)

	form, ok := r.Form("page.form")
	if !ok {
		t.Fatal("page.form not registered")
	}
	if form.Name != "page.form" {
		t.Errorf("form.Name = %q, want %q", form.Name, "page.form")
	}
	if len(form.Fields) != 4 {
		t.Fatalf("fields count = %d, want 4", len(form.Fields))
	}

	wantNames := []string{"title", "slug", "content", "is_active"}
	for i, want := range wantNames {
		if form.Fields[i].Name != want {
			t.Errorf("field[%d].Name = %q, want %q", i, form.Fields[i].Name, want)
		}
	}

	if !form.Fields[0].Required {
		t.Error("title field should be required")
	}
	if !form.Fields[1].Required {
		t.Error("slug field should be required")
	}
	if form.Fields[2].Required {
		t.Error("content field should not be required")
	}

	activeField := form.Fields[3]
	if activeField.Type != "checkbox" {
		t.Errorf("is_active type = %q, want %q", activeField.Type, "checkbox")
	}
	if activeField.Default != true {
		t.Errorf("is_active default = %v, want true", activeField.Default)
	}
}

func TestRegisterPageSchemas_Grid(t *testing.T) {
	r := admin.NewRegistry()
	adminApp.RegisterPageSchemas(r)

	grid, ok := r.Grid("page.grid")
	if !ok {
		t.Fatal("page.grid not registered")
	}
	if grid.Name != "page.grid" {
		t.Errorf("grid.Name = %q, want %q", grid.Name, "page.grid")
	}
	if len(grid.Columns) != 6 {
		t.Fatalf("columns count = %d, want 6", len(grid.Columns))
	}

	wantCols := []string{"id", "title", "slug", "is_active", "created_at", "updated_at"}
	for i, want := range wantCols {
		if grid.Columns[i].Name != want {
			t.Errorf("column[%d].Name = %q, want %q", i, grid.Columns[i].Name, want)
		}
	}
}
