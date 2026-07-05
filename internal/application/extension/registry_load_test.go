package extension_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
)

type stubFieldRepo struct {
	fields []domainext.ExtensionField
}

func (s *stubFieldRepo) Save(context.Context, domainext.ExtensionField) error {
	panic("not implemented")
}

func (s *stubFieldRepo) FindByCode(context.Context, string) (domainext.ExtensionField, error) {
	panic("not implemented")
}

func (s *stubFieldRepo) ListActive(_ context.Context, scope domainext.TargetType) ([]domainext.ExtensionField, error) {
	if scope == "" {
		return append([]domainext.ExtensionField(nil), s.fields...), nil
	}
	out := make([]domainext.ExtensionField, 0)
	for _, field := range s.fields {
		if field.Scope == scope {
			out = append(out, field)
		}
	}
	return out, nil
}

func (s *stubFieldRepo) SoftDelete(context.Context, string) error {
	panic("not implemented")
}

func TestRegistry_LoadPersisted_MergesWithPluginFields(t *testing.T) {
	reg := extension.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.plugin.field",
		Label:       "Plugin field",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("Register plugin field: %v", err)
	}

	repo := &stubFieldRepo{fields: []domainext.ExtensionField{
		{
			Code:        "acme.plugin.field",
			Label:       "Persisted duplicate",
			Type:        domainext.FieldTypeString,
			Scope:       domainext.TargetProduct,
			StorageMode: domainext.StorageStored,
			Visibility:  domainext.VisibilityPublic,
		},
		{
			Code:        "acme.db.field",
			Label:       "Persisted field",
			Type:        domainext.FieldTypeString,
			Scope:       domainext.TargetProduct,
			StorageMode: domainext.StorageStored,
			Visibility:  domainext.VisibilityPublic,
		},
	}}

	if err := reg.LoadPersisted(context.Background(), repo); err != nil {
		t.Fatalf("LoadPersisted: %v", err)
	}

	pluginField, ok := reg.Get("acme.plugin.field")
	if !ok {
		t.Fatal("expected plugin field to remain registered")
	}
	if pluginField.Label != "Plugin field" {
		t.Errorf("plugin field label = %q, want plugin registration to win", pluginField.Label)
	}
	if _, ok := reg.Get("acme.db.field"); !ok {
		t.Fatal("expected persisted field to be merged")
	}
	if reg.Len() != 2 {
		t.Fatalf("Len = %d, want 2", reg.Len())
	}
}
