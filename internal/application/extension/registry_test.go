package extension_test

import (
	"errors"
	"fmt"
	"sync"
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
	if !errors.Is(err, extension.ErrAlreadyRegistered) {
		t.Fatalf("expected ErrAlreadyRegistered, got %v", err)
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

func TestRegistry_RegisterOwned_RecordsAndClearsOwner(t *testing.T) {
	reg := extension.NewRegistry()
	def := domainext.FieldDef{Code: "acme.owned", Label: "O", Type: domainext.FieldTypeString, Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored}
	if err := reg.RegisterOwned(def, "acme"); err != nil {
		t.Fatal(err)
	}
	if got := reg.Owner("acme.owned"); got != "acme" {
		t.Fatalf("Owner = %q", got)
	}
	if got := reg.Owner("missing"); got != "" {
		t.Fatalf("Owner(missing) = %q", got)
	}
	plain := def
	plain.Code = "acme.plain"
	if err := reg.Register(plain); err != nil {
		t.Fatal(err)
	}
	if got := reg.Owner("acme.plain"); got != "" {
		t.Fatalf("unowned Owner = %q", got)
	}
	reg.Remove("acme.owned")
	if got := reg.Owner("acme.owned"); got != "" {
		t.Fatalf("Owner after Remove = %q", got)
	}
	if err := reg.RegisterOwned(def, "other"); err != nil || reg.Owner("acme.owned") != "other" {
		t.Fatalf("re-register after Remove: err=%v owner=%q", err, reg.Owner("acme.owned"))
	}
}

func TestRegistry_OwnedAndUnownedDuplicateCodes(t *testing.T) {
	reg := extension.NewRegistry()
	def := func(code string) domainext.FieldDef {
		return domainext.FieldDef{Code: code, Label: "L", Type: domainext.FieldTypeString, Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored}
	}
	if err := reg.Register(def("a.free")); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterOwned(def("a.free"), "acme"); !errors.Is(err, extension.ErrAlreadyRegistered) {
		t.Fatalf("owned over unowned err = %v", err)
	}
	if err := reg.RegisterOwned(def("b.owned"), "acme"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(def("b.owned")); !errors.Is(err, extension.ErrAlreadyRegistered) {
		t.Fatalf("unowned over owned err = %v", err)
	}
	if reg.Owner("a.free") != "" || reg.Owner("b.owned") != "acme" {
		t.Fatalf("owners changed: %q %q", reg.Owner("a.free"), reg.Owner("b.owned"))
	}
}

func TestRegistry_OwnerConcurrentAccess(t *testing.T) {
	reg := extension.NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code := fmt.Sprintf("c%d.field", i)
			for n := 0; n < 50; n++ {
				_ = reg.RegisterOwned(domainext.FieldDef{Code: code, Label: "L", Type: domainext.FieldTypeString, Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored}, "acme")
				_ = reg.Owner(code)
				reg.Remove(code)
			}
		}(i)
	}
	wg.Wait()
}
