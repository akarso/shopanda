package slots_test

import (
	"reflect"
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
)

func TestUnknownDeclaredAnchors(t *testing.T) {
	unknown := slots.UnknownDeclaredAnchors([]string{
		"pdp.info",
		"custom.widget",
		"pdp.info",
		"another.custom",
	})
	want := []string{"another.custom", "custom.widget"}
	if !reflect.DeepEqual(unknown, want) {
		t.Fatalf("unknown = %v, want %v", unknown, want)
	}
}

func TestUnknownDeclaredAnchors_DefaultCatalogOnly(t *testing.T) {
	if unknown := slots.UnknownDeclaredAnchors(slots.StandardAnchorNames()); len(unknown) != 0 {
		t.Fatalf("standard anchors should not be unknown: %v", unknown)
	}
}
