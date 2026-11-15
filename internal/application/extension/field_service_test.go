package extension_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type memFieldRepo struct {
	fields       map[string]domainext.ExtensionField
	softDeleted  map[string]domainext.ExtensionField
	valueCounts  map[string]int64
	findErr      error
	saveErr      error
	deleteErr    error
	retireErr    error
	purgeErr     error
	purgePartial int64 // committed rows when purgeErr is set
	saveCalls    int
}

func newMemFieldRepo() *memFieldRepo {
	return &memFieldRepo{
		fields:      make(map[string]domainext.ExtensionField),
		softDeleted: make(map[string]domainext.ExtensionField),
		valueCounts: make(map[string]int64),
	}
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
	field, ok := m.fields[code]
	if !ok {
		return apperror.NotFound("extension field not found")
	}
	if field.Access.PluginOnlyWrite {
		return apperror.PluginOnlyFieldImmutable("plugin-only extension field cannot be deleted")
	}
	delete(m.fields, code)
	m.softDeleted[code] = field
	return nil
}

func (m *memFieldRepo) Retire(_ context.Context, code string) error {
	if m.retireErr != nil {
		return m.retireErr
	}
	field, ok := m.fields[code]
	if !ok {
		return apperror.NotFound("extension field not found")
	}
	if !field.Access.PluginOnlyWrite {
		return apperror.Validation("extension field is not plugin-only; use admin DELETE for ordinary fields")
	}
	delete(m.fields, code)
	m.softDeleted[code] = field
	return nil
}

func (m *memFieldRepo) RetireActive(_ context.Context, code string, purgeValues bool) (int64, error) {
	if m.retireErr != nil {
		return 0, m.retireErr
	}
	field, ok := m.fields[code]
	if !ok {
		return 0, apperror.NotFound("extension field not found")
	}
	if !field.Access.PluginOnlyWrite {
		return 0, apperror.Validation("extension field is not plugin-only; use admin DELETE for ordinary fields")
	}
	delete(m.fields, code)
	m.softDeleted[code] = field
	if !purgeValues {
		return 0, nil
	}
	if m.purgeErr != nil {
		return m.purgePartial, fmt.Errorf("%w: %v", domainext.ErrPurgeIncomplete, m.purgeErr)
	}
	purged := m.valueCounts[code]
	delete(m.valueCounts, code)
	return purged, nil
}

func (m *memFieldRepo) PurgeIfSoftDeletedPluginOnly(_ context.Context, code string) (int64, error) {
	field, ok := m.softDeleted[code]
	if !ok || !field.Access.PluginOnlyWrite {
		return 0, apperror.NotFound("extension field not found")
	}
	if _, active := m.fields[code]; active {
		return 0, apperror.NotFound("extension field not found")
	}
	if m.purgeErr != nil {
		partial := m.purgePartial
		return partial, fmt.Errorf("%w: %v", domainext.ErrPurgeIncomplete, m.purgeErr)
	}
	purged := m.valueCounts[code]
	delete(m.valueCounts, code)
	return purged, nil
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

func TestFieldService_Retire_PluginOnlyRemovesDefinitionAndRegistry(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	repo.valueCounts["acme.locked"] = 3
	svc := extensionapp.NewFieldService(reg, repo)
	def := serviceFieldDef("acme.locked")
	def.Access = domainext.Access{PluginOnlyWrite: true}
	if _, err := svc.Create(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), "acme.locked"); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Delete err = %v, want ErrFieldPluginOnly", err)
	}

	if _, err := svc.Retire(context.Background(), "acme.locked", extensionapp.RetireOptions{
		PurgeValues: true,
		Store:       repo,
	}); !errors.Is(err, extensionapp.ErrPurgeWhileActive) {
		t.Fatalf("purge while active err = %v, want ErrPurgeWhileActive", err)
	}
	if _, err := repo.FindByCode(context.Background(), "acme.locked"); err != nil {
		t.Fatalf("active row must remain after refused purge: %v", err)
	}

	got, err := svc.Retire(context.Background(), "acme.locked", extensionapp.RetireOptions{Store: repo})
	if err != nil {
		t.Fatalf("soft-delete Retire: %v", err)
	}
	if got.Code != "acme.locked" || got.ValuesPurged != 0 {
		t.Fatalf("soft-delete result = %+v", got)
	}
	got, err = svc.Retire(context.Background(), "acme.locked", extensionapp.RetireOptions{
		PurgeValues: true,
		Store:       repo,
	})
	if err != nil {
		t.Fatalf("purge Retire: %v", err)
	}
	if !got.PurgeOnly || got.ValuesPurged != 3 {
		t.Fatalf("purge result = %+v", got)
	}
	if _, ok := reg.Get("acme.locked"); ok {
		t.Fatal("expected registry entry removed")
	}
	if _, err := repo.FindByCode(context.Background(), "acme.locked"); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("repo after retire: %v", err)
	}
}

func TestFieldService_Retire_OrdinaryFieldRefused(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)
	if _, err := svc.Create(context.Background(), serviceFieldDef("acme.plain")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Retire(context.Background(), "acme.plain", extensionapp.RetireOptions{Store: repo}); !errors.Is(err, extensionapp.ErrFieldNotPluginOnly) {
		t.Fatalf("Retire err = %v, want ErrFieldNotPluginOnly", err)
	}
	if _, ok := reg.Get("acme.plain"); !ok {
		t.Fatal("ordinary field must remain registered")
	}
}

func TestFieldService_Retire_MissingAndStoreRequired(t *testing.T) {
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(extensionapp.NewRegistry(), repo)
	if _, err := svc.Retire(context.Background(), "acme.missing", extensionapp.RetireOptions{Store: repo}); !errors.Is(err, extensionapp.ErrFieldNotFound) {
		t.Fatalf("missing err = %v, want ErrFieldNotFound", err)
	}
	if _, err := svc.Retire(context.Background(), "acme.any", extensionapp.RetireOptions{PurgeValues: true}); err == nil {
		t.Fatal("expected error when Store is nil")
	}
}

func TestFieldService_Retire_PurgeFailureKeepsSoftDeleteThenRetry(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	repo.valueCounts["acme.locked"] = 2
	svc := extensionapp.NewFieldService(reg, repo)
	def := serviceFieldDef("acme.locked")
	def.Access = domainext.Access{PluginOnlyWrite: true}
	if _, err := svc.Create(context.Background(), def); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Retire(context.Background(), "acme.locked", extensionapp.RetireOptions{Store: repo}); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}
	if _, ok := reg.Get("acme.locked"); ok {
		t.Fatal("registry must drop the field after soft-delete commits")
	}

	boom := errors.New("purge boom")
	repo.purgeErr = boom
	repo.purgePartial = 0
	got, err := svc.Retire(context.Background(), "acme.locked", extensionapp.RetireOptions{PurgeValues: true, Store: repo})
	if !errors.Is(err, domainext.ErrPurgeIncomplete) {
		t.Fatalf("first purge err = %v, want ErrPurgeIncomplete", err)
	}
	if _, err := repo.FindByCode(context.Background(), "acme.locked"); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("definition must stay soft-deleted after purge failure: %v", err)
	}
	if repo.valueCounts["acme.locked"] != 2 {
		t.Fatalf("values must remain for retry, got %d", repo.valueCounts["acme.locked"])
	}
	if got.Code != "acme.locked" || got.ValuesPurged != 0 || !got.PurgeOnly {
		t.Fatalf("result = %+v", got)
	}

	repo.purgeErr = nil
	got, err = svc.Retire(context.Background(), "acme.locked", extensionapp.RetireOptions{PurgeValues: true, Store: repo})
	if err != nil {
		t.Fatalf("retry Retire: %v", err)
	}
	if !got.PurgeOnly || got.ValuesPurged != 2 {
		t.Fatalf("retry result = %+v", got)
	}
	if _, ok := repo.valueCounts["acme.locked"]; ok {
		t.Fatal("values must be gone after retry purge")
	}
}

func TestFieldService_Retire_PurgeOnlyIncompleteKeepsValuesPurged(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMemFieldRepo()
	locked, err := domainext.NewExtensionField(serviceFieldDef("acme.locked"))
	if err != nil {
		t.Fatal(err)
	}
	locked.Access.PluginOnlyWrite = true
	repo.softDeleted["acme.locked"] = locked
	repo.valueCounts["acme.locked"] = 5
	repo.purgeErr = context.DeadlineExceeded
	repo.purgePartial = 2
	svc := extensionapp.NewFieldService(reg, repo)

	got, err := svc.Retire(context.Background(), "acme.locked", extensionapp.RetireOptions{PurgeValues: true, Store: repo})
	if !errors.Is(err, domainext.ErrPurgeIncomplete) {
		t.Fatalf("err = %v, want ErrPurgeIncomplete", err)
	}
	if !got.PurgeOnly || got.ValuesPurged != 2 {
		t.Fatalf("result = %+v, want PurgeOnly ValuesPurged=2", got)
	}
}

// restoreRaceRepo simulates Save restoring an ordinary definition between the
// service's FindByCode(not found) and PurgeIfSoftDeletedPluginOnly's assert.
type restoreRaceRepo struct {
	findCalls int
	restored  domainext.ExtensionField
	purged    int64
}

func (r *restoreRaceRepo) Create(context.Context, domainext.ExtensionField) error { return nil }
func (r *restoreRaceRepo) Save(context.Context, domainext.ExtensionField) error   { return nil }
func (r *restoreRaceRepo) ListActive(context.Context, domainext.TargetType) ([]domainext.ExtensionField, error) {
	return nil, nil
}
func (r *restoreRaceRepo) SoftDelete(context.Context, string) error { return nil }
func (r *restoreRaceRepo) Retire(context.Context, string) error     { return nil }
func (r *restoreRaceRepo) FindByCode(_ context.Context, code string) (domainext.ExtensionField, error) {
	r.findCalls++
	if r.findCalls == 1 {
		return domainext.ExtensionField{}, apperror.NotFound("extension field not found")
	}
	return r.restored, nil
}
func (r *restoreRaceRepo) RetireActive(context.Context, string, bool) (int64, error) {
	return 0, nil
}
func (r *restoreRaceRepo) PurgeIfSoftDeletedPluginOnly(context.Context, string) (int64, error) {
	return r.purged, apperror.NotFound("extension field not found")
}

func TestFieldService_Retire_PurgeAssertRaceOrdinaryRestore(t *testing.T) {
	plain, err := domainext.NewExtensionField(serviceFieldDef("acme.restored"))
	if err != nil {
		t.Fatal(err)
	}
	repo := &restoreRaceRepo{restored: plain, purged: 4}
	svc := extensionapp.NewFieldService(extensionapp.NewRegistry(), repo)

	got, err := svc.Retire(context.Background(), "acme.restored", extensionapp.RetireOptions{
		PurgeValues: true,
		Store:       repo,
	})
	if !errors.Is(err, extensionapp.ErrFieldNotPluginOnly) {
		t.Fatalf("err = %v, want ErrFieldNotPluginOnly (ordinary restore)", err)
	}
	if !got.PurgeOnly || got.ValuesPurged != 4 {
		t.Fatalf("result = %+v, want PurgeOnly ValuesPurged=4", got)
	}
	if repo.findCalls != 2 {
		t.Fatalf("FindByCode calls = %d, want 2", repo.findCalls)
	}
}

func TestFieldService_Retire_PurgeAssertRacePluginOnlyRestore(t *testing.T) {
	locked, err := domainext.NewExtensionField(serviceFieldDef("acme.restored.po"))
	if err != nil {
		t.Fatal(err)
	}
	locked.Access.PluginOnlyWrite = true
	repo := &restoreRaceRepo{restored: locked, purged: 2}
	svc := extensionapp.NewFieldService(extensionapp.NewRegistry(), repo)

	got, err := svc.Retire(context.Background(), "acme.restored.po", extensionapp.RetireOptions{
		PurgeValues: true,
		Store:       repo,
	})
	if !errors.Is(err, extensionapp.ErrPurgeWhileActive) {
		t.Fatalf("err = %v, want ErrPurgeWhileActive", err)
	}
	if !got.PurgeOnly || got.ValuesPurged != 2 {
		t.Fatalf("result = %+v, want PurgeOnly ValuesPurged=2", got)
	}
}

type purgeNotFoundWithCountStore struct {
	restoreRaceRepo
	purged int64
}

func (s *purgeNotFoundWithCountStore) FindByCode(context.Context, string) (domainext.ExtensionField, error) {
	// Always not found (soft-deleted non-plugin-only / gone).
	return domainext.ExtensionField{}, apperror.NotFound("extension field not found")
}
func (s *purgeNotFoundWithCountStore) PurgeIfSoftDeletedPluginOnly(context.Context, string) (int64, error) {
	return s.purged, apperror.NotFound("extension field not found")
}

func TestFieldService_Retire_PurgeNotFoundKeepsValuesPurged(t *testing.T) {
	repo := &purgeNotFoundWithCountStore{purged: 3}
	svc := extensionapp.NewFieldService(extensionapp.NewRegistry(), repo)

	got, err := svc.Retire(context.Background(), "acme.gone", extensionapp.RetireOptions{
		PurgeValues: true,
		Store:       repo,
	})
	if !errors.Is(err, extensionapp.ErrFieldNotFound) {
		t.Fatalf("err = %v, want ErrFieldNotFound", err)
	}
	if !got.PurgeOnly || got.ValuesPurged != 3 {
		t.Fatalf("result = %+v, want PurgeOnly ValuesPurged=3", got)
	}
}

type purgeNotFoundThenFindErrRepo struct {
	purgeNotFoundWithCountStore
	rereadErr error
	findN     int
}

func (r *purgeNotFoundThenFindErrRepo) FindByCode(context.Context, string) (domainext.ExtensionField, error) {
	r.findN++
	if r.findN == 1 {
		return domainext.ExtensionField{}, apperror.NotFound("extension field not found")
	}
	return domainext.ExtensionField{}, r.rereadErr
}

func TestFieldService_Retire_PurgeRereadDBErrorKeepsValuesPurged(t *testing.T) {
	boom := errors.New("db timeout")
	repo := &purgeNotFoundThenFindErrRepo{
		purgeNotFoundWithCountStore: purgeNotFoundWithCountStore{purged: 2},
		rereadErr:                   boom,
	}
	svc := extensionapp.NewFieldService(extensionapp.NewRegistry(), repo)

	got, err := svc.Retire(context.Background(), "acme.flaky", extensionapp.RetireOptions{
		PurgeValues: true,
		Store:       repo,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want db timeout", err)
	}
	if !got.PurgeOnly || got.ValuesPurged != 2 {
		t.Fatalf("result = %+v, want PurgeOnly ValuesPurged=2", got)
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

func TestFieldService_AdminUpdateKeepsOwnerAndDeleteClearsIt(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.RegisterOwned(serviceFieldDef("acme.label"), "acme"); err != nil {
		t.Fatal(err)
	}
	svc := extensionapp.NewFieldService(reg, newMemFieldRepo())
	def := serviceFieldDef("acme.label")
	def.Label = "Renamed"
	if _, err := svc.Update(context.Background(), "acme.label", def); err != nil {
		t.Fatal(err)
	}
	if got := reg.Owner("acme.label"); got != "acme" {
		t.Fatalf("owner after admin Update = %q, want acme", got)
	}
	if err := svc.Delete(context.Background(), "acme.label"); err != nil {
		t.Fatal(err)
	}
	if got := reg.Owner("acme.label"); got != "" {
		t.Fatalf("owner after Delete = %q, want empty", got)
	}
}

func TestRegistry_Replace_PluginOnlyRolesAndValidationStayMutable(t *testing.T) {
	// Deliberate: only the shape that reinterprets stored values is frozen.
	reg := extensionapp.NewRegistry()
	def := serviceFieldDef("acme.locked")
	def.Access = domainext.Access{PluginOnlyWrite: true}
	if err := reg.RegisterOwned(def, "acme"); err != nil {
		t.Fatal(err)
	}
	f, _ := reg.Get("acme.locked")
	f.Access.ReadRoles = []string{"admin"}
	f.Access.WriteRoles = []string{"admin"}
	f.Validation = domainext.Validation{Required: true}
	if err := reg.Replace(f); err != nil {
		t.Fatalf("Replace roles/validation: %v", err)
	}
}

func TestRegistry_Replace_NonPluginOnlyMayBecomePluginOnly(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(serviceFieldDef("acme.open")); err != nil {
		t.Fatal(err)
	}
	f, _ := reg.Get("acme.open")
	f.Access.PluginOnlyWrite = true
	if err := reg.Replace(f); err != nil {
		t.Fatalf("false->true flip: %v", err)
	}
	f.Access.PluginOnlyWrite = false
	if err := reg.Replace(f); !errors.Is(err, extensionapp.ErrPluginOnlyImmutable) {
		t.Fatalf("true->false flip err = %v", err)
	}
}

// Pins that guardPluginOnly (service) and ValidateReplace (registry) agree.
func TestFieldService_UpdateAgreesWithValidateReplace(t *testing.T) {
	reg := extensionapp.NewRegistry()
	def := serviceFieldDef("acme.locked")
	def.Access = domainext.Access{PluginOnlyWrite: true}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	repo := newMemFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)
	changed := def
	changed.Type = domainext.FieldTypeBool
	nf, err := domainext.NewExtensionField(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.ValidateReplace(nf); !errors.Is(err, extensionapp.ErrPluginOnlyImmutable) {
		t.Fatalf("ValidateReplace err = %v", err)
	}
	if _, err := svc.Update(context.Background(), "acme.locked", changed); !errors.Is(err, extensionapp.ErrFieldPluginOnly) {
		t.Fatalf("Update err = %v", err)
	}
	if _, err := repo.FindByCode(context.Background(), "acme.locked"); err == nil {
		t.Fatal("nothing may be persisted when the update is refused")
	}
}

// lockedFieldRepo serialises the unsynchronised in-memory fake for -race runs.
type lockedFieldRepo struct {
	mu sync.Mutex
	r  *memFieldRepo
}

func (l *lockedFieldRepo) Create(ctx context.Context, f domainext.ExtensionField) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Create(ctx, f)
}
func (l *lockedFieldRepo) Save(ctx context.Context, f domainext.ExtensionField) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Save(ctx, f)
}
func (l *lockedFieldRepo) FindByCode(ctx context.Context, c string) (domainext.ExtensionField, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.FindByCode(ctx, c)
}
func (l *lockedFieldRepo) ListActive(ctx context.Context, s domainext.TargetType) ([]domainext.ExtensionField, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.ListActive(ctx, s)
}
func (l *lockedFieldRepo) SoftDelete(ctx context.Context, c string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.SoftDelete(ctx, c)
}
func (l *lockedFieldRepo) Retire(ctx context.Context, c string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Retire(ctx, c)
}

func TestFieldService_ConcurrentUpdateAndRegistryMutation(t *testing.T) {
	reg := extensionapp.NewRegistry()
	svc := extensionapp.NewFieldService(reg, &lockedFieldRepo{r: newMemFieldRepo()})
	ctx := context.Background()
	if _, err := svc.Create(ctx, serviceFieldDef("acme.open")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			d := serviceFieldDef("acme.open")
			d.Label = "L2"
			_, _ = svc.Update(ctx, "acme.open", d)
		}()
		go func() {
			defer wg.Done()
			if f, ok := reg.Get("acme.open"); ok {
				_ = reg.Replace(f)
			}
			reg.Remove("acme.open")
		}()
	}
	wg.Wait()
}
