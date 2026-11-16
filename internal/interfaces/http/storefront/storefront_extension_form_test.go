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

func (storefrontFormTestValueRepo) UpsertBatchSelective(ctx context.Context, values []domainext.Value, exclusiveFieldCodes map[string]struct{}) error {
	return storefrontFormTestValueRepo{}.UpsertBatch(ctx, values)
}

func (storefrontFormTestValueRepo) Delete(_ context.Context, _ domainext.Target, _ string) error {
	return apperror.NotFound("extension value not found")
}

func (storefrontFormTestValueRepo) ListTargetIDsByTypeAndIDPrefix(context.Context, domainext.TargetType, string) ([]string, error) {
	return nil, nil
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

func TestStorefrontCartExtensionFields_OmitsPluginOnly(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift message", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.print.design", Label: "Print design", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageSnapshot,
		Access: domainext.Access{PluginOnlyWrite: true},
	}); err != nil {
		t.Fatal(err)
	}
	h := &StorefrontHandler{extensions: extensionapp.NewValueService(reg, storefrontFormTestValueRepo{})}
	fields := h.storefrontCartExtensionFields()
	if len(fields) != 1 || fields[0].Code != "acme.gift.message" {
		t.Fatalf("fields = %+v, want only the customer-writable field", fields)
	}
}

func TestStorefrontExtensionInputsFromForm_DropsPluginOnlyField(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift message", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.print.design", Label: "Print design", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageSnapshot,
		Access: domainext.Access{PluginOnlyWrite: true},
	}); err != nil {
		t.Fatal(err)
	}
	h := &StorefrontHandler{extensions: extensionapp.NewValueService(reg, storefrontFormTestValueRepo{})}
	body := "ext__acme-gift-message=hello&ext__acme-print-design="
	req := httptest.NewRequest("POST", "/cart/add", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	inputs := h.storefrontExtensionInputsFromForm(req)
	if len(inputs) != 1 || inputs[0].FieldCode != "acme.gift.message" || inputs[0].Value != "hello" {
		t.Fatalf("inputs = %+v, want only the customer field", inputs)
	}
}
