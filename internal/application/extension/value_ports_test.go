package extension_test

import (
	"reflect"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
)

// Request-facing handlers depend on ValueAPI. None of the intentionally ungated
// ValueService methods may ever appear on it.
func TestValueAPI_ExcludesUngatedMethods(t *testing.T) {
	ungated := []string{"DeleteAllForTarget", "DeleteOrphanCartItemExtensions", "CopyTarget", "SnapshotCartItemToOrderItem"}
	api := reflect.TypeOf((*extensionapp.ValueAPI)(nil)).Elem()
	for _, name := range ungated {
		if _, ok := api.MethodByName(name); ok {
			t.Fatalf("ValueAPI must not expose ungated method %s", name)
		}
	}
	svc := reflect.TypeOf(&extensionapp.ValueService{})
	for _, name := range ungated { // guards against a rename silently emptying the list above
		if _, ok := svc.MethodByName(name); !ok {
			t.Fatalf("ValueService no longer has %s: update the ungated list", name)
		}
	}
}

func TestAsValueAPI_NilPointerIsNilInterface(t *testing.T) {
	var nilSvc *extensionapp.ValueService
	if extensionapp.AsValueAPI(nilSvc) != nil {
		t.Fatal("nil *ValueService must become a nil interface")
	}
	if extensionapp.AsValueAPI(&extensionapp.ValueService{}) == nil {
		t.Fatal("non-nil service must stay non-nil")
	}
}
