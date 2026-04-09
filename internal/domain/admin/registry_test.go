package admin_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/admin"
)

func TestRegisterForm_and_Retrieve(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Name", Required: true},
		},
	})

	f, ok := r.Form("product.form")
	if !ok {
		t.Fatal("expected form to be found")
	}
	if f.Name != "product.form" {
		t.Errorf("Name = %q, want %q", f.Name, "product.form")
	}
	if len(f.Fields) != 1 {
		t.Fatalf("Fields len = %d, want 1", len(f.Fields))
	}
	if f.Fields[0].Name != "name" {
		t.Errorf("Fields[0].Name = %q, want %q", f.Fields[0].Name, "name")
	}
}

func TestForm_NotFound(t *testing.T) {
	r := admin.NewRegistry()
	_, ok := r.Form("missing")
	if ok {
		t.Error("expected ok=false for unregistered form")
	}
}

func TestRegisterFormField_Appends(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Name"},
		},
	})

	err := r.RegisterFormField("product.form", admin.Field{
		Name: "origin", Type: "text", Label: "Country of Origin",
	})
	if err != nil {
		t.Fatalf("RegisterFormField: %v", err)
	}

	f, _ := r.Form("product.form")
	if len(f.Fields) != 2 {
		t.Fatalf("Fields len = %d, want 2", len(f.Fields))
	}
	if f.Fields[1].Name != "origin" {
		t.Errorf("Fields[1].Name = %q, want %q", f.Fields[1].Name, "origin")
	}
}

func TestRegisterFormField_UnknownForm(t *testing.T) {
	r := admin.NewRegistry()
	err := r.RegisterFormField("missing", admin.Field{Name: "x"})
	if err == nil {
		t.Fatal("expected error for unknown form")
	}
}

func TestRegisterGrid_and_Retrieve(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("product.grid", admin.Grid{
		Columns: []admin.Column{
			{Name: "name", Label: "Name"},
		},
	})

	g, ok := r.Grid("product.grid")
	if !ok {
		t.Fatal("expected grid to be found")
	}
	if g.Name != "product.grid" {
		t.Errorf("Name = %q, want %q", g.Name, "product.grid")
	}
	if len(g.Columns) != 1 {
		t.Fatalf("Columns len = %d, want 1", len(g.Columns))
	}
}

func TestGrid_NotFound(t *testing.T) {
	r := admin.NewRegistry()
	_, ok := r.Grid("missing")
	if ok {
		t.Error("expected ok=false for unregistered grid")
	}
}

func TestRegisterGridColumn_Appends(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("product.grid", admin.Grid{})

	err := r.RegisterGridColumn("product.grid", admin.Column{
		Name: "price", Label: "Price",
	})
	if err != nil {
		t.Fatalf("RegisterGridColumn: %v", err)
	}

	g, _ := r.Grid("product.grid")
	if len(g.Columns) != 1 {
		t.Fatalf("Columns len = %d, want 1", len(g.Columns))
	}
	if g.Columns[0].Name != "price" {
		t.Errorf("Columns[0].Name = %q, want %q", g.Columns[0].Name, "price")
	}
}

func TestRegisterGridColumn_UnknownGrid(t *testing.T) {
	r := admin.NewRegistry()
	err := r.RegisterGridColumn("missing", admin.Column{Name: "x"})
	if err == nil {
		t.Fatal("expected error for unknown grid")
	}
}

func TestRegisterAction_Appends(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("product.grid", admin.Grid{})

	err := r.RegisterAction("product.grid", admin.Action{
		Name: "delete", Label: "Delete",
	})
	if err != nil {
		t.Fatalf("RegisterAction: %v", err)
	}

	g, _ := r.Grid("product.grid")
	if len(g.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(g.Actions))
	}
	if g.Actions[0].Name != "delete" {
		t.Errorf("Actions[0].Name = %q, want %q", g.Actions[0].Name, "delete")
	}
}

func TestRegisterAction_UnknownGrid(t *testing.T) {
	r := admin.NewRegistry()
	err := r.RegisterAction("missing", admin.Action{Name: "x"})
	if err == nil {
		t.Fatal("expected error for unknown grid")
	}
}

func TestRegisterForm_Replaces(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{{Name: "old"}},
	})
	r.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{{Name: "new"}},
	})

	f, _ := r.Form("product.form")
	if len(f.Fields) != 1 || f.Fields[0].Name != "new" {
		t.Errorf("expected replaced form, got Fields = %v", f.Fields)
	}
}
