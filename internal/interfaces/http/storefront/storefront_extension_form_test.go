package storefront

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type storefrontFormTestValueRepo struct{}

func (storefrontFormTestValueRepo) ListByTarget(_ context.Context, _ domainext.Target) ([]domainext.Value, error) {
	return nil, nil
}

func (storefrontFormTestValueRepo) ListByTargets(_ context.Context, _ domainext.TargetType, _ []string) ([]domainext.Value, error) {
	return nil, nil
}

func (storefrontFormTestValueRepo) Upsert(_ context.Context, _ domainext.Value) error {
	return nil
}

func (storefrontFormTestValueRepo) UpsertBatch(_ context.Context, _ []domainext.Value) error {
	return nil
}

func (storefrontFormTestValueRepo) Delete(_ context.Context, _ domainext.Target, _ string) error {
	return apperror.NotFound("extension value not found")
}

func TestStorefrontExtensionFormNameRoundTrip(t *testing.T) {
	cases := []string{
		"acme.gift.message",
		"acme.gift_message",
		"acme.wrap_level",
	}
	for _, code := range cases {
		name := storefrontExtensionFormName(code)
		got, ok := storefrontExtensionCodeFromFormName(name)
		if !ok || got != code {
			t.Errorf("round trip %q: name=%q got=%q ok=%v", code, name, got, ok)
		}
	}
}

func TestStorefrontExtensionInputsFromForm_BoolUsesLastValue(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.wrap",
		Label:       "Gift wrap",
		Type:        domainext.FieldTypeBool,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	values := extensionapp.NewValueService(reg, storefrontFormTestValueRepo{})
	h := &StorefrontHandler{extensions: values}

	formName := storefrontExtensionFormName("acme.gift.wrap")
	req := httptest.NewRequest("POST", "/cart/add", strings.NewReader(formName+"=false&"+formName+"=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}

	inputs := h.storefrontExtensionInputsFromForm(req)
	if len(inputs) != 1 {
		t.Fatalf("inputs = %+v, want 1", inputs)
	}
	b, ok := inputs[0].Value.(bool)
	if !ok || !b {
		t.Fatalf("value = %#v (%T), want bool true", inputs[0].Value, inputs[0].Value)
	}
}
