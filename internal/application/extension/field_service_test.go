package extension_test

import (
	"context"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type memFieldRepo struct {
	fields map[string]domainext.ExtensionField
}

func newMemFieldRepo() *memFieldRepo {
	return &memFieldRepo{fields: make(map[string]domainext.ExtensionField)}
}

func (m *memFieldRepo) Create(_ context.Context, field domainext.ExtensionField) error {
	if _, ok := m.fields[field.Code]; ok {
		return apperror.Conflict("extension field already exists")
	}
	m.fields[field.Code] = field
	return nil
}

func (m *memFieldRepo) Save(_ context.Context, field domainext.ExtensionField) error {
	m.fields[field.Code] = field
	return nil
}

func (m *memFieldRepo) FindByCode(_ context.Context, code string) (domainext.ExtensionField, error) {
	field, ok := m.fields[code]
	if !ok {
		return domainext.ExtensionField{}, apperror.NotFound("extension field not found")
	}
	return field, nil
}

func (m *memFieldRepo) ListActive(_ context.Context, scope domainext.TargetType) ([]domainext.ExtensionField, error) {
	out := make([]domainext.ExtensionField, 0)
	for _, field := range m.fields {
		if scope != "" && field.Scope != scope {
			continue
		}
		out = append(out, field)
	}
	return out, nil
}

func (m *memFieldRepo) SoftDelete(_ context.Context, code string) error {
	if _, ok := m.fields[code]; !ok {
		return apperror.NotFound("extension field not found")
	}
	delete(m.fields, code)
	return nil
}

func serviceFieldDef(code string) domainext.FieldDef {
	return domainext.FieldDef{
		Code:        code,
		Label:       "Sample",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	}
}

func TestFieldService_CreateAndGet(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)

	field, err := svc.Create(context.Background(), serviceFieldDef("acme.sample.field"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := svc.Get(field.Code, false)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Code != field.Code {
		t.Fatalf("Get = %+v", got)
	}
}

func TestFieldService_CreateDuplicateConflict(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)
	def := serviceFieldDef("acme.dup.field")

	if _, err := svc.Create(context.Background(), def); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := svc.Create(context.Background(), def)
	if err != extensionapp.ErrFieldAlreadyExists {
		t.Fatalf("duplicate Create err = %v, want ErrFieldAlreadyExists", err)
	}
}

func TestFieldService_PrivateFieldHiddenWithoutInclude(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)

	def := serviceFieldDef("acme.private.field")
	def.Visibility = domainext.VisibilityPrivate
	if _, err := svc.Create(context.Background(), def); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := svc.Get("acme.private.field", false)
	if err != extensionapp.ErrFieldNotFound {
		t.Fatalf("Get private without include err = %v", err)
	}
	got, err := svc.Get("acme.private.field", true)
	if err != nil {
		t.Fatalf("Get private with include: %v", err)
	}
	if got.Visibility != domainext.VisibilityPrivate {
		t.Fatalf("visibility = %q", got.Visibility)
	}
}

func TestFieldService_DeleteRemovesFromRegistry(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)

	field, err := svc.Create(context.Background(), serviceFieldDef("acme.delete.field"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(context.Background(), field.Code); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := reg.Get(field.Code); ok {
		t.Fatal("field still in registry after delete")
	}
}

func TestFieldService_UpdateSuccess(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)

	created, err := svc.Create(context.Background(), serviceFieldDef("acme.update.field"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updateDef := created.ToFieldDef()
	updateDef.Label = "Updated label"
	updated, err := svc.Update(context.Background(), created.Code, updateDef)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Label != "Updated label" {
		t.Fatalf("Label = %q, want Updated label", updated.Label)
	}
	got, err := svc.Get(created.Code, false)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Label != "Updated label" {
		t.Fatalf("registry label = %q", got.Label)
	}
}

func TestFieldService_UpdateNotFound(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)

	_, err := svc.Update(context.Background(), "acme.missing.field", serviceFieldDef("acme.missing.field"))
	if err != extensionapp.ErrFieldNotFound {
		t.Fatalf("Update err = %v, want ErrFieldNotFound", err)
	}
}

func TestFieldService_UpdateRegistersWhenMissingFromRegistry(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)

	field, err := domainext.NewExtensionField(serviceFieldDef("acme.repo.only"))
	if err != nil {
		t.Fatalf("NewExtensionField: %v", err)
	}
	if err := repo.Save(context.Background(), field); err != nil {
		t.Fatalf("repo Save: %v", err)
	}

	updateDef := field.ToFieldDef()
	updateDef.Label = "Repo restored"
	updated, err := svc.Update(context.Background(), field.Code, updateDef)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Label != "Repo restored" {
		t.Fatalf("Label = %q", updated.Label)
	}
	if _, ok := reg.Get(field.Code); !ok {
		t.Fatal("expected field registered after update fallback")
	}
}
