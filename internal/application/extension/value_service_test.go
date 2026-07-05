package extension_test

import (
	"context"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type memValueRepo struct {
	values map[string]domainext.Value
}

func valueKey(target domainext.Target, fieldCode string) string {
	return string(target.Type) + ":" + target.ID + ":" + fieldCode
}

func newMemValueRepo() *memValueRepo {
	return &memValueRepo{values: make(map[string]domainext.Value)}
}

func (m *memValueRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, value := range m.values {
		if value.TargetType == target.Type && value.TargetID == target.ID {
			out = append(out, value)
		}
	}
	return out, nil
}

func (m *memValueRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[valueKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)] = value
	return nil
}

func (m *memValueRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := valueKey(target, fieldCode)
	if _, ok := m.values[key]; !ok {
		return apperror.NotFound("extension value not found")
	}
	delete(m.values, key)
	return nil
}

func registerValueField(t *testing.T, reg *extensionapp.Registry, def domainext.FieldDef) {
	t.Helper()
	if err := reg.Register(def); err != nil {
		t.Fatalf("register field %q: %v", def.Code, err)
	}
}

func TestValueService_UpsertBatchAndList(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.note",
		Label:       "Note",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	})
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	stored, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "acme.note", Value: "hello"},
	}, "admin-1", false)
	if err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "hello" {
		t.Fatalf("stored = %+v", stored)
	}

	values, err := svc.List(context.Background(), target, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(values) != 1 || values[0].FieldCode != "acme.note" {
		t.Fatalf("List = %+v", values)
	}
}

func TestValueService_PrivateFieldWriteDenied(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.secret",
		Label:       "Secret",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Visibility:  domainext.VisibilityPrivate,
	})
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	_, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "acme.secret", Value: "hidden"},
	}, "admin-1", false)
	if err == nil || err != domainext.ErrForbiddenPrivateField {
		t.Fatalf("UpsertBatch err = %v, want ErrForbiddenPrivateField", err)
	}
}

func TestValueService_UnknownFieldCode(t *testing.T) {
	reg := extensionapp.NewRegistry()
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	_, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "missing.field", Value: "x"},
	}, "admin-1", false)
	if err == nil || err != domainext.ErrUnknownFieldCode {
		t.Fatalf("UpsertBatch err = %v, want ErrUnknownFieldCode", err)
	}
}

func TestValueService_ValidationFailed(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.count",
		Label:       "Count",
		Type:        domainext.FieldTypeInt,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Validation: domainext.Validation{
			Required: true,
			Min:      int64Ptr(1),
		},
	})
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	_, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "acme.count", Value: 0},
	}, "admin-1", false)
	if err == nil || !domainext.IsValidationError(err) {
		t.Fatalf("UpsertBatch err = %v, want validation error", err)
	}
}

func TestValueService_ListFiltersPrivateUnlessAllowed(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.public",
		Label:       "Public",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	})
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.private",
		Label:       "Private",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Visibility:  domainext.VisibilityPrivate,
	})
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	if _, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "acme.public", Value: "visible"},
		{FieldCode: "acme.private", Value: "hidden"},
	}, "admin-1", true); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	values, err := svc.List(context.Background(), target, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(values) != 1 || values[0].FieldCode != "acme.public" {
		t.Fatalf("List without private = %+v", values)
	}

	values, err = svc.List(context.Background(), target, true)
	if err != nil {
		t.Fatalf("List include private: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("List with private = %+v", values)
	}
}

func TestValueService_Delete(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.note",
		Label:       "Note",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	})
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	target := domainext.Target{Type: domainext.TargetProduct, ID: "prod-1"}

	if _, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "acme.note", Value: "hello"},
	}, "admin-1", false); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
	if err := svc.Delete(context.Background(), target, "acme.note", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	values, err := svc.List(context.Background(), target, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("List after delete = %+v", values)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
