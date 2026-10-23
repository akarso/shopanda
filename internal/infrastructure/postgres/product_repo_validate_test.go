package postgres

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestValidateProduct_MapsDomainErrors(t *testing.T) {
	t.Parallel()

	p, err := catalog.NewProduct("p1", "Widget", "widget")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if err := validateProduct(&p); err != nil {
		t.Fatalf("valid product: %v", err)
	}

	p.Type = catalog.Type("kit")
	err = validateProduct(&p)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("invalid type: got %v, want validation", err)
	}
	if err == nil || !strings.Contains(err.Error(), `"kit"`) {
		t.Fatalf("error = %v, want quoted type", err)
	}

	p.Type = ""
	err = validateProduct(&p)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty type: got %v, want validation", err)
	}

	err = validateProduct(nil)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("nil: got %v, want validation", err)
	}

	p.Type = catalog.TypeSimple
	p.VisibilityModes = catalog.DefaultVisibilityAxes()
	p.VisibilityModes.Purchasable = catalog.VisibilityMode("nope")
	err = validateProduct(&p)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("invalid visibility mode: got %v, want validation", err)
	}
}
