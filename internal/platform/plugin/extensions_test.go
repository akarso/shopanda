package plugin_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_Extensions_RegisterField(t *testing.T) {
	reg := extension.NewRegistry()
	app := &plugin.App{}
	app.SetExtensionRegistry(reg)

	err := app.Extensions().RegisterField(domainext.FieldDef{
		Code:        "acme.gift.wrap_level",
		Label:       "Gift wrap",
		Type:        domainext.FieldTypeEnum,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Validation:  domainext.EnumOptions("none", "standard"),
	})
	if err != nil {
		t.Fatalf("RegisterField: %v", err)
	}

	field, ok := app.ExtensionRegistry().Get("acme.gift.wrap_level")
	if !ok {
		t.Fatal("expected field in shared registry")
	}
	if field.Scope != domainext.TargetProduct {
		t.Errorf("Scope = %q", field.Scope)
	}
}

func TestApp_Extensions_RegisterFieldDuplicate(t *testing.T) {
	app := &plugin.App{}
	def := domainext.FieldDef{
		Code:        "acme.gift.wrap_level",
		Label:       "Gift wrap",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	}
	if err := app.Extensions().RegisterField(def); err != nil {
		t.Fatalf("first RegisterField: %v", err)
	}
	err := app.Extensions().RegisterField(def)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestApp_SetExtensionRegistry_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil extension registry")
		}
	}()
	app := &plugin.App{}
	app.SetExtensionRegistry(nil)
}
