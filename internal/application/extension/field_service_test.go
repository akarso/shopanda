package extension_test

import (
	"context"
	"errors"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type memFieldRepo struct {
	fields    map[string]domainext.ExtensionField
	findErr   error
	saveErr   error
	deleteErr error
	saveCalls int
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
	m.saveCalls++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.fields[field.Code] = field
	return nil
}

func (m *memFieldRepo) FindByCode(_ context.Context, code string) (domainext.ExtensionField, error) {
	if m.findErr != nil {
		return domainext.ExtensionField{}, m.findErr
	}
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
	if m.deleteErr != nil {
		return m.deleteErr
	}
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

func TestFieldService_PluginOnlyFieldIsImmutableViaService(t *testing.T) {
	reg := extensionapp.NewRegistry()
	def := serviceFieldDef("acme.locked")
	def.Access = domainext.Access{PluginOnlyWrite: true}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	svc := extensionapp.NewFieldService(reg, newMemFieldRepo())

	unlocked := def
	unlocked.Access = domainext.Access{}
	if _, err := svc.Update(context.Background(), "acme.locked", unlocked); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Update err = %v, want ErrFieldPluginOnly", err)
	}
	if err := svc.Delete(context.Background(), "acme.locked"); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Delete err = %v, want ErrFieldPluginOnly", err)
	}
	got, ok := reg.Get("acme.locked")
	if !ok || !got.Access.PluginOnlyWrite {
		t.Fatalf("registry field changed: %+v ok=%v", got, ok)
	}
}

func TestFieldService_GuardUsesRepoWhenRegistryDrifted(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	locked := domainext.ExtensionField{Code: "acme.locked", Label: "L", Type: domainext.FieldTypeString,
		Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored, Visibility: domainext.VisibilityPublic,
		Access: domainext.Access{PluginOnlyWrite: true}}
	repo.fields[locked.Code] = locked // in DB, not in this instance's registry
	svc := extensionapp.NewFieldService(reg, repo)

	if err := svc.Delete(context.Background(), "acme.locked"); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Delete err = %v, want ErrFieldPluginOnly", err)
	}
	def := serviceFieldDef("acme.locked")
	if _, err := svc.Update(context.Background(), "acme.locked", def); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Update err = %v, want ErrFieldPluginOnly", err)
	}
	if !repo.fields["acme.locked"].Access.PluginOnlyWrite {
		t.Fatal("repo row must be unchanged")
	}
}

// Documents the one-way latch: an ordinary field can be made plugin-only, never reverted via the API.
func TestFieldService_MakingFieldPluginOnlyIsOneWay(t *testing.T) {
	reg := extensionapp.NewRegistry()
	svc := extensionapp.NewFieldService(reg, newMemFieldRepo())
	def := serviceFieldDef("acme.latch")
	if _, err := svc.Create(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	def.Access = domainext.Access{PluginOnlyWrite: true}
	if _, err := svc.Update(context.Background(), "acme.latch", def); err != nil {
		t.Fatalf("lock: %v", err)
	}
	def.Access = domainext.Access{}
	if _, err := svc.Update(context.Background(), "acme.latch", def); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("revert err = %v, want ErrFieldPluginOnly", err)
	}
}

func TestFieldService_GuardFailsClosedOnRepoError(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(serviceFieldDef("acme.plain")); err != nil {
		t.Fatal(err)
	}
	repo := newMemFieldRepo()
	boom := errors.New("db timeout")
	repo.findErr = boom
	svc := extensionapp.NewFieldService(reg, repo)

	if _, err := svc.Update(context.Background(), "acme.plain", serviceFieldDef("acme.plain")); !errors.Is(err, boom) {
		t.Fatalf("Update err = %v, want repo error", err)
	}
	if err := svc.Delete(context.Background(), "acme.plain"); !errors.Is(err, boom) {
		t.Fatalf("Delete err = %v, want repo error", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("Save called %d times despite guard error", repo.saveCalls)
	}
}

// The repo enforces the guard atomically; a concurrent flag flip surfaces as the repo error.
func TestFieldService_RepoImmutableErrorMapsToErrFieldPluginOnly(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(serviceFieldDef("acme.race")); err != nil {
		t.Fatal(err)
	}
	repo := newMemFieldRepo()
	repo.saveErr = apperror.PluginOnlyFieldImmutable("locked concurrently")
	repo.deleteErr = apperror.PluginOnlyFieldImmutable("locked concurrently")
	svc := extensionapp.NewFieldService(reg, repo)
	if _, err := svc.Update(context.Background(), "acme.race", serviceFieldDef("acme.race")); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Update err = %v", err)
	}
	if err := svc.Delete(context.Background(), "acme.race"); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Delete err = %v", err)
	}
}
