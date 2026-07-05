package extension_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

func sampleFieldDef() domainext.FieldDef {
	return domainext.FieldDef{
		Code:        "acme.gift.wrap_level",
		Label:       "Gift wrap",
		Type:        domainext.FieldTypeEnum,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Validation:  domainext.EnumOptions("none", "standard"),
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := extension.NewRegistry()
	if err := reg.Register(sampleFieldDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	field, ok := reg.Get("acme.gift.wrap_level")
	if !ok {
		t.Fatal("expected field to be registered")
	}
	if field.Label != "Gift wrap" {
		t.Errorf("Label = %q", field.Label)
	}
}

func TestRegistry_DuplicateCodeRejected(t *testing.T) {
	reg := extension.NewRegistry()
	if err := reg.Register(sampleFieldDef()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := reg.Register(sampleFieldDef())
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRegistry_ListFiltersPrivateByDefault(t *testing.T) {
	reg := extension.NewRegistry()
	public := sampleFieldDef()
	private := sampleFieldDef()
	private.Code = "acme.internal.token"
	private.Visibility = domainext.VisibilityPrivate

	if err := reg.Register(public); err != nil {
		t.Fatalf("Register public: %v", err)
	}
	if err := reg.Register(private); err != nil {
		t.Fatalf("Register private: %v", err)
	}

	fields := reg.List(extension.ListFilter{})
	if len(fields) != 1 {
		t.Fatalf("List len = %d, want 1 public field", len(fields))
	}
	if fields[0].Code != public.Code {
		t.Errorf("List[0].Code = %q", fields[0].Code)
	}

	all := reg.List(extension.ListFilter{IncludePrivate: true})
	if len(all) != 2 {
		t.Fatalf("IncludePrivate List len = %d, want 2", len(all))
	}
}

func TestRegistry_ListByScope(t *testing.T) {
	reg := extension.NewRegistry()
	product := sampleFieldDef()
	cart := sampleFieldDef()
	cart.Code = "acme.gift.message"
	cart.Scope = domainext.TargetCartItem
	cart.Type = domainext.FieldTypeString
	cart.Validation = domainext.Validation{}

	if err := reg.Register(product); err != nil {
		t.Fatalf("Register product: %v", err)
	}
	if err := reg.Register(cart); err != nil {
		t.Fatalf("Register cart: %v", err)
	}

	fields := reg.List(extension.ListFilter{Scope: domainext.TargetCartItem})
	if len(fields) != 1 || fields[0].Code != cart.Code {
		t.Fatalf("List by scope = %+v, want cart field only", fields)
	}
}

func TestRegistry_RegisterValidationError(t *testing.T) {
	reg := extension.NewRegistry()
	def := sampleFieldDef()
	def.Code = "invalid"
	err := reg.Register(def)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
