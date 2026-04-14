package admin_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/rbac"
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

func TestRegisterGrid_Replaces(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("product.grid", admin.Grid{
		Columns: []admin.Column{{Name: "old"}},
	})
	r.RegisterGrid("product.grid", admin.Grid{
		Columns: []admin.Column{{Name: "new"}},
	})

	g, _ := r.Grid("product.grid")
	if len(g.Columns) != 1 {
		t.Fatalf("Columns len = %d, want 1", len(g.Columns))
	}
	if g.Columns[0].Name != "new" {
		t.Errorf("Columns[0].Name = %q, want %q", g.Columns[0].Name, "new")
	}
}

func TestForm_ReturnedValueIsImmutable(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("f", admin.Form{
		Fields: []admin.Field{{
			Name: "original",
			Meta: map[string]interface{}{"nested": map[string]interface{}{"key": "value"}},
		}},
	})

	f, _ := r.Form("f")
	f.Fields[0].Name = "mutated"
	f.Fields[0].Meta["nested"].(map[string]interface{})["key"] = "changed"
	f.Fields = append(f.Fields, admin.Field{Name: "extra"})

	f2, _ := r.Form("f")
	if len(f2.Fields) != 1 {
		t.Fatalf("Fields len = %d, want 1", len(f2.Fields))
	}
	if f2.Fields[0].Name != "original" {
		t.Errorf("Fields[0].Name = %q, want %q", f2.Fields[0].Name, "original")
	}
	v, ok := f2.Fields[0].Meta["nested"]
	if !ok {
		t.Fatal("expected Meta[nested] to exist")
	}
	nested, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("Meta[nested] type = %T, want map[string]interface{}", v)
	}
	if nested["key"] != "value" {
		t.Errorf("nested Meta leaked: got %q, want %q", nested["key"], "value")
	}
}

func TestGrid_ReturnedValueIsImmutable(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("g", admin.Grid{
		Columns: []admin.Column{{
			Name: "original",
			Meta: map[string]interface{}{"nested": map[string]interface{}{"key": "value"}},
		}},
	})

	g, _ := r.Grid("g")
	g.Columns[0].Name = "mutated"
	g.Columns[0].Meta["nested"].(map[string]interface{})["key"] = "changed"
	g.Columns = append(g.Columns, admin.Column{Name: "extra"})

	g2, _ := r.Grid("g")
	if len(g2.Columns) != 1 {
		t.Fatalf("Columns len = %d, want 1", len(g2.Columns))
	}
	if g2.Columns[0].Name != "original" {
		t.Errorf("Columns[0].Name = %q, want %q", g2.Columns[0].Name, "original")
	}
	v, ok := g2.Columns[0].Meta["nested"]
	if !ok {
		t.Fatal("expected Meta[nested] to exist")
	}
	nested, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("Meta[nested] type = %T, want map[string]interface{}", v)
	}
	if nested["key"] != "value" {
		t.Errorf("nested Meta leaked: got %q, want %q", nested["key"], "value")
	}
}

func TestForm_MetaSliceIsImmutable(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("f", admin.Form{
		Fields: []admin.Field{{
			Name: "original",
			Meta: map[string]interface{}{"tags": []interface{}{"value"}},
		}},
	})

	f, _ := r.Form("f")
	sl := f.Fields[0].Meta["tags"].([]interface{})
	sl[0] = "changed"
	f.Fields[0].Meta["tags"] = append(sl, "extra")

	f2, _ := r.Form("f")
	v, ok := f2.Fields[0].Meta["tags"]
	if !ok {
		t.Fatal("expected Meta[tags] to exist")
	}
	tags, ok := v.([]interface{})
	if !ok {
		t.Fatalf("Meta[tags] type = %T, want []interface{}", v)
	}
	if len(tags) != 1 {
		t.Fatalf("tags len = %d, want 1", len(tags))
	}
	if tags[0] != "value" {
		t.Errorf("tags[0] = %v, want %q", tags[0], "value")
	}
}

func TestGrid_MetaSliceIsImmutable(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("g", admin.Grid{
		Columns: []admin.Column{{
			Name: "original",
			Meta: map[string]interface{}{"tags": []interface{}{"value"}},
		}},
	})

	g, _ := r.Grid("g")
	sl := g.Columns[0].Meta["tags"].([]interface{})
	sl[0] = "changed"
	g.Columns[0].Meta["tags"] = append(sl, "extra")

	g2, _ := r.Grid("g")
	v, ok := g2.Columns[0].Meta["tags"]
	if !ok {
		t.Fatal("expected Meta[tags] to exist")
	}
	tags, ok := v.([]interface{})
	if !ok {
		t.Fatalf("Meta[tags] type = %T, want []interface{}", v)
	}
	if len(tags) != 1 {
		t.Fatalf("tags len = %d, want 1", len(tags))
	}
	if tags[0] != "value" {
		t.Errorf("tags[0] = %v, want %q", tags[0], "value")
	}
}

func TestForm_OptionsIsImmutable(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("f", admin.Form{
		Fields: []admin.Field{{
			Name:    "colour",
			Options: []admin.Option{{Label: "Red", Value: "red"}},
		}},
	})

	f, _ := r.Form("f")
	f.Fields[0].Options[0].Label = "mutated"
	f.Fields[0].Options = append(f.Fields[0].Options, admin.Option{Label: "Blue", Value: "blue"})

	f2, _ := r.Form("f")
	if len(f2.Fields[0].Options) != 1 {
		t.Fatalf("Options len = %d, want 1", len(f2.Fields[0].Options))
	}
	if f2.Fields[0].Options[0].Label != "Red" {
		t.Errorf("Options[0].Label = %q, want %q", f2.Fields[0].Options[0].Label, "Red")
	}
}

func TestGrid_ActionsIsImmutable(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("g", admin.Grid{
		Actions: []admin.Action{{Name: "delete", Label: "Delete"}},
	})

	g, _ := r.Grid("g")
	g.Actions[0].Label = "mutated"
	g.Actions = append(g.Actions, admin.Action{Name: "archive", Label: "Archive"})

	g2, _ := r.Grid("g")
	if len(g2.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(g2.Actions))
	}
	if g2.Actions[0].Label != "Delete" {
		t.Errorf("Actions[0].Label = %q, want %q", g2.Actions[0].Label, "Delete")
	}
}

func TestSetFormPermission(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("product.form", admin.Form{})

	err := r.SetFormPermission("product.form", rbac.ProductsWrite)
	if err != nil {
		t.Fatalf("SetFormPermission: %v", err)
	}

	p, ok := r.FormPermission("product.form")
	if !ok {
		t.Fatal("expected permission to be found")
	}
	if p != rbac.ProductsWrite {
		t.Errorf("permission = %q, want %q", p, rbac.ProductsWrite)
	}
}

func TestSetFormPermission_UnknownForm(t *testing.T) {
	r := admin.NewRegistry()
	err := r.SetFormPermission("missing", rbac.ProductsRead)
	if err == nil {
		t.Fatal("expected error for unknown form")
	}
}

func TestFormPermission_NotSet(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("product.form", admin.Form{})

	_, ok := r.FormPermission("product.form")
	if ok {
		t.Error("expected ok=false when no permission set")
	}
}

func TestSetGridPermission(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("product.grid", admin.Grid{})

	err := r.SetGridPermission("product.grid", rbac.ProductsRead)
	if err != nil {
		t.Fatalf("SetGridPermission: %v", err)
	}

	p, ok := r.GridPermission("product.grid")
	if !ok {
		t.Fatal("expected permission to be found")
	}
	if p != rbac.ProductsRead {
		t.Errorf("permission = %q, want %q", p, rbac.ProductsRead)
	}
}

func TestSetGridPermission_UnknownGrid(t *testing.T) {
	r := admin.NewRegistry()
	err := r.SetGridPermission("missing", rbac.ProductsRead)
	if err == nil {
		t.Fatal("expected error for unknown grid")
	}
}

func TestSetFormPermission_EmptyPerm(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterForm("product.form", admin.Form{})
	err := r.SetFormPermission("product.form", "")
	if err == nil {
		t.Fatal("expected error for empty permission")
	}
}

func TestSetGridPermission_EmptyPerm(t *testing.T) {
	r := admin.NewRegistry()
	r.RegisterGrid("product.grid", admin.Grid{})
	err := r.SetGridPermission("product.grid", "")
	if err == nil {
		t.Fatal("expected error for empty permission")
	}
}
