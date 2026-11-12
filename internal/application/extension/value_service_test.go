package extension_test

import (
	"context"
	"sort"
	"strings"
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

func (m *memValueRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, targetID := range targetIDs {
		stored, err := m.ListByTarget(context.Background(), domainext.Target{Type: targetType, ID: targetID})
		if err != nil {
			return nil, err
		}
		out = append(out, stored...)
	}
	return out, nil
}

func (m *memValueRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[valueKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)] = value
	return nil
}

func (m *memValueRepo) UpsertBatch(_ context.Context, values []domainext.Value) error {
	for _, value := range values {
		if err := m.Upsert(context.Background(), value); err != nil {
			return err
		}
	}
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

func (m *memValueRepo) ListTargetIDsByTypeAndIDPrefix(_ context.Context, targetType domainext.TargetType, idPrefix string) ([]string, error) {
	prefix := strings.TrimSuffix(idPrefix, "%")
	seen := make(map[string]struct{})
	var out []string
	for _, value := range m.values {
		if value.TargetType != targetType {
			continue
		}
		if !strings.HasPrefix(value.TargetID, prefix) {
			continue
		}
		if _, ok := seen[value.TargetID]; ok {
			continue
		}
		seen[value.TargetID] = struct{}{}
		out = append(out, value.TargetID)
	}
	sort.Strings(out)
	return out, nil
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

func TestValueService_EmptyUpdatedByRejected(t *testing.T) {
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

	_, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "acme.note", Value: "hello"},
	}, "   ", false)
	if err == nil || !domainext.IsValidationError(err) {
		t.Fatalf("UpsertBatch err = %v, want validation error for empty updated_by", err)
	}
}

func TestValueService_DuplicateFieldCodeRejected(t *testing.T) {
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

	_, err := svc.UpsertBatch(context.Background(), target, []domainext.ValueInput{
		{FieldCode: "acme.note", Value: "first"},
		{FieldCode: "acme.note", Value: "second"},
	}, "admin-1", false)
	if err == nil || !domainext.IsValidationError(err) {
		t.Fatalf("UpsertBatch err = %v, want validation error for duplicate field_code", err)
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

func TestValueService_DeleteOrphanCartItemExtensions(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	})
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	ctx := context.Background()
	cartID := "cart-1"

	for _, spec := range []struct {
		variantID string
		value     string
	}{
		{"var-1", "keep"},
		{"var-2", "drop"},
	} {
		target := domainext.CartItemTarget(cartID, spec.variantID)
		if _, err := svc.UpsertBatch(ctx, target, []domainext.ValueInput{
			{FieldCode: "acme.gift.message", Value: spec.value},
		}, "guest", false); err != nil {
			t.Fatalf("UpsertBatch %s: %v", spec.variantID, err)
		}
	}

	if err := svc.DeleteOrphanCartItemExtensions(ctx, cartID, []string{"var-1"}); err != nil {
		t.Fatalf("DeleteOrphanCartItemExtensions: %v", err)
	}
	stale, err := repo.ListByTarget(ctx, domainext.CartItemTarget(cartID, "var-2"))
	if err != nil {
		t.Fatalf("ListByTarget dropped: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("dropped target = %+v, want empty", stale)
	}
	kept, err := repo.ListByTarget(ctx, domainext.CartItemTarget(cartID, "var-1"))
	if err != nil {
		t.Fatalf("ListByTarget kept: %v", err)
	}
	if len(kept) != 1 {
		t.Fatalf("kept target = %+v, want one row", kept)
	}
	if err := svc.DeleteOrphanCartItemExtensions(ctx, cartID, []string{"var-1"}); err != nil {
		t.Fatalf("idempotent DeleteOrphanCartItemExtensions: %v", err)
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

func pluginOnlyField(code string) domainext.FieldDef {
	return domainext.FieldDef{
		Code:        code,
		Label:       "Plugin only",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
		Access:      domainext.Access{PluginOnlyWrite: true},
	}
}

func TestValueService_PluginOnlyWrite_UntrustedDenied(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, pluginOnlyField("acme.design"))
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	target := domainext.CartItemTarget("cart-1", "var-1")
	in := []domainext.ValueInput{{FieldCode: "acme.design", Value: "d1"}}

	if err := svc.ValidateBatch(context.Background(), target, in, false); err != domainext.ErrForbiddenPluginOnlyField {
		t.Fatalf("ValidateBatch err = %v, want ErrForbiddenPluginOnlyField", err)
	}
	if _, err := svc.UpsertBatch(context.Background(), target, in, "cust-1", false); err != domainext.ErrForbiddenPluginOnlyField {
		t.Fatalf("UpsertBatch err = %v, want ErrForbiddenPluginOnlyField", err)
	}
	if len(repo.values) != 0 {
		t.Fatalf("stored = %+v, want none", repo.values)
	}
	if !apperror.Is(extensionapp.MapValueError(domainext.ErrForbiddenPluginOnlyField), apperror.CodeForbiddenPluginOnlyField) {
		t.Fatal("MapValueError must map to forbidden_plugin_only_field")
	}
}

func TestValueService_PluginOnlyWrite_TrustedContextAllowed(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, pluginOnlyField("acme.design"))
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	target := domainext.CartItemTarget("cart-1", "var-1")
	ctx := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design")

	if _, err := svc.UpsertBatch(ctx, target, []domainext.ValueInput{{FieldCode: "acme.design", Value: "d1"}}, "plugin:acme", false); err != nil {
		t.Fatalf("UpsertBatch trusted: %v", err)
	}
	if len(repo.values) != 1 {
		t.Fatalf("stored = %d, want 1", len(repo.values))
	}
	for _, v := range repo.values {
		if v.UpdatedBy != "plugin:acme" {
			t.Fatalf("UpdatedBy = %q, want plugin:acme", v.UpdatedBy)
		}
	}
}

func TestValueService_PluginOnlyWrite_DeleteAndCopyStayAllowed(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, pluginOnlyField("acme.design"))
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	src := domainext.CartItemTarget("cart-1", "var-1")
	dst := domainext.CartItemTarget("cart-2", "var-1")
	trusted := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design")
	if _, err := svc.UpsertBatch(trusted, src, []domainext.ValueInput{{FieldCode: "acme.design", Value: "d1"}}, "plugin:acme", false); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := svc.CopyTarget(ctx, src, dst, "system"); err != nil {
		t.Fatalf("CopyTarget: %v", err)
	}
	if err := svc.SnapshotCartItemToOrderItem(ctx, "cart-2", "ord-1", "var-1", "system"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got, _ := repo.ListByTarget(ctx, domainext.OrderItemTarget("ord-1", "var-1")); len(got) != 1 {
		t.Fatalf("order item values = %d, want 1", len(got))
	}
	if err := svc.DeleteAllForTarget(ctx, src); err != nil {
		t.Fatalf("DeleteAllForTarget: %v", err)
	}
	if got, _ := repo.ListByTarget(ctx, src); len(got) != 0 {
		t.Fatalf("after delete = %+v", got)
	}
}

func TestValueService_NonPluginOnlyFieldUnchanged(t *testing.T) {
	reg := extensionapp.NewRegistry()
	def := pluginOnlyField("acme.note")
	def.Access = domainext.Access{}
	registerValueField(t, reg, def)
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	if _, err := svc.UpsertBatch(context.Background(), domainext.CartItemTarget("c", "v"), []domainext.ValueInput{{FieldCode: "acme.note", Value: "x"}}, "cust", false); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
}

func TestValueService_PluginOnlyWrite_DeleteRequiresTrust(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, pluginOnlyField("acme.design"))
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	target := domainext.CartItemTarget("cart-1", "var-1")
	trusted := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design")
	if _, err := svc.UpsertBatch(trusted, target, []domainext.ValueInput{{FieldCode: "acme.design", Value: "d1"}}, "x", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), target, "acme.design", true); err != domainext.ErrForbiddenPluginOnlyField {
		t.Fatalf("untrusted Delete err = %v, want ErrForbiddenPluginOnlyField", err)
	}
	if len(repo.values) != 1 {
		t.Fatalf("value must survive untrusted delete, have %d", len(repo.values))
	}
	if err := svc.Delete(trusted, target, "acme.design", false); err != nil {
		t.Fatalf("trusted Delete: %v", err)
	}
	if len(repo.values) != 0 {
		t.Fatalf("value must be gone after trusted delete")
	}
}

func TestValueService_PluginOnlyWrite_TrustIsPerFieldCode(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, pluginOnlyField("acme.design"))
	registerValueField(t, reg, pluginOnlyField("other.flag"))
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	target := domainext.CartItemTarget("cart-1", "var-1")
	ctx := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design")

	_, err := svc.UpsertBatch(ctx, target, []domainext.ValueInput{
		{FieldCode: "acme.design", Value: "d1"},
		{FieldCode: "other.flag", Value: "forged"},
	}, "x", false)
	if err != domainext.ErrForbiddenPluginOnlyField {
		t.Fatalf("err = %v, want ErrForbiddenPluginOnlyField for field not named in context", err)
	}
	if len(repo.values) != 0 {
		t.Fatalf("stored = %+v, want none", repo.values)
	}
}

func TestWithPluginWrite_PanicsOnEmptyRegistrantOrCodes(t *testing.T) {
	for name, fn := range map[string]func(){
		"empty registrant": func() { extensionapp.WithPluginWrite(context.Background(), "  ", "a.b") },
		"no codes":         func() { extensionapp.WithPluginWrite(context.Background(), "acme") },
		"blank code":       func() { extensionapp.WithPluginWrite(context.Background(), "acme", " ") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: expected panic", name)
				}
			}()
			fn()
		}()
	}
}

func TestStripPluginWrite_RemovesTrust(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, pluginOnlyField("acme.design"))
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	target := domainext.CartItemTarget("c", "v")
	in := []domainext.ValueInput{{FieldCode: "acme.design", Value: "x"}}
	trusted := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design")
	if _, err := svc.UpsertBatch(extensionapp.StripPluginWrite(trusted), target, in, "x", false); err != domainext.ErrForbiddenPluginOnlyField {
		t.Fatalf("stripped ctx err = %v, want forbidden", err)
	}
}

func TestValueService_PluginOnlyWrite_TrustSurvivesWithoutCancel(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerValueField(t, reg, pluginOnlyField("acme.design"))
	svc := extensionapp.NewValueService(reg, newMemValueRepo())
	ctx, cancel := context.WithCancel(extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design"))
	cancel()
	if _, err := svc.UpsertBatch(context.WithoutCancel(ctx), domainext.CartItemTarget("c", "v"),
		[]domainext.ValueInput{{FieldCode: "acme.design", Value: "x"}}, "x", false); err != nil {
		t.Fatalf("WithoutCancel must keep trust: %v", err)
	}
}

func TestValueService_PluginOnlyWrite_OwnerIsEnforced(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.RegisterOwned(pluginOnlyField("b.field"), "plugin-b"); err != nil {
		t.Fatal(err)
	}
	registerValueField(t, reg, pluginOnlyField("free.field")) // unowned: any trusted registrant
	repo := newMemValueRepo()
	svc := extensionapp.NewValueService(reg, repo)
	target := domainext.CartItemTarget("c", "v")
	in := func(code string) []domainext.ValueInput { return []domainext.ValueInput{{FieldCode: code, Value: "x"}} }

	other := extensionapp.WithPluginWrite(context.Background(), "plugin-a", "b.field", "free.field")
	if _, err := svc.UpsertBatch(other, target, in("b.field"), "x", false); err != domainext.ErrForbiddenPluginOnlyField {
		t.Fatalf("other plugin write err = %v, want ErrForbiddenPluginOnlyField", err)
	}
	if err := svc.Delete(other, target, "b.field", false); err != domainext.ErrForbiddenPluginOnlyField {
		t.Fatalf("other plugin delete err = %v, want ErrForbiddenPluginOnlyField", err)
	}
	if _, err := svc.UpsertBatch(other, target, in("free.field"), "x", false); err != nil {
		t.Fatalf("unowned field must stay writable by any trusted registrant: %v", err)
	}
	owner := extensionapp.WithPluginWrite(context.Background(), "plugin-b", "b.field")
	if _, err := svc.UpsertBatch(owner, target, in("b.field"), "x", false); err != nil {
		t.Fatalf("owner write: %v", err)
	}
	if got := repo.values[valueKey(target, "b.field")].UpdatedBy; got != "plugin:plugin-b" {
		t.Fatalf("UpdatedBy = %q", got)
	}
}
