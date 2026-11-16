package cart_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
)

type memCartExtensionValueRepo struct {
	values map[string]domainext.Value
}

func cartExtKey(target domainext.Target, fieldCode string) string {
	return string(target.Type) + ":" + target.ID + ":" + fieldCode
}

func newMemCartExtensionValueRepo() *memCartExtensionValueRepo {
	return &memCartExtensionValueRepo{values: make(map[string]domainext.Value)}
}

func (m *memCartExtensionValueRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, value := range m.values {
		if value.TargetType == target.Type && value.TargetID == target.ID {
			out = append(out, value)
		}
	}
	return out, nil
}

func (m *memCartExtensionValueRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
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

func (m *memCartExtensionValueRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[cartExtKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)] = value
	return nil
}

func (m *memCartExtensionValueRepo) UpsertBatch(_ context.Context, values []domainext.Value) error {
	for _, value := range values {
		if err := m.Upsert(context.Background(), value); err != nil {
			return err
		}
	}
	return nil
}

func (m *memCartExtensionValueRepo) UpsertBatchSelective(_ context.Context, values []domainext.Value, exclusiveFieldCodes map[string]struct{}) error {
	for _, value := range values {
		if _, ok := exclusiveFieldCodes[value.FieldCode]; !ok || value.Payload.StringValue == nil {
			continue
		}
		key := cartExtKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)
		if existing, ok := m.values[key]; ok && existing.Payload.StringValue != nil {
			stored := strings.TrimSpace(*existing.Payload.StringValue)
			want := strings.TrimSpace(*value.Payload.StringValue)
			if stored != "" && stored != want {
				return domainext.ErrStringValueConflict
			}
		}
	}
	return m.UpsertBatch(context.Background(), values)
}

func (m *memCartExtensionValueRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := cartExtKey(target, fieldCode)
	if _, ok := m.values[key]; !ok {
		return apperror.NotFound("extension value not found")
	}
	delete(m.values, key)
	return nil
}

func (m *memCartExtensionValueRepo) ListTargetIDsByTypeAndIDPrefix(_ context.Context, targetType domainext.TargetType, idPrefix string) ([]string, error) {
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

type orphanExtensionPurge struct {
	cartID         string
	keepVariantIDs []string
}

type trackingExtWriter struct {
	inner           *extensionapp.ValueService
	failDeleteCount int
	deleteErr       error
	deleted         []string
	copied          int
	orphanPurges    []orphanExtensionPurge
}

func (w *trackingExtWriter) IsPluginOnlyWrite(code string) bool {
	if w.inner == nil {
		return false
	}
	return w.inner.IsPluginOnlyWrite(code)
}

func (w *trackingExtWriter) ValidateBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, canAccessPrivate bool) error {
	if w.inner == nil {
		return nil
	}
	return w.inner.ValidateBatch(ctx, target, inputs, canAccessPrivate)
}

func (w *trackingExtWriter) UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error) {
	if w.inner == nil {
		return nil, nil
	}
	return w.inner.UpsertBatch(ctx, target, inputs, updatedBy, canAccessPrivate)
}

func (w *trackingExtWriter) UpsertBatchSelective(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool, exclusiveFieldCodes map[string]struct{}) ([]domainext.Value, error) {
	if w.inner == nil {
		return nil, nil
	}
	return w.inner.UpsertBatchSelective(ctx, target, inputs, updatedBy, canAccessPrivate, exclusiveFieldCodes)
}

func (w *trackingExtWriter) Delete(ctx context.Context, target domainext.Target, fieldCode string, canAccessPrivate bool) error {
	if w.inner == nil {
		return nil
	}
	return w.inner.Delete(ctx, target, fieldCode, canAccessPrivate)
}

func (w *trackingExtWriter) DeleteAllForTarget(ctx context.Context, target domainext.Target) error {
	w.deleted = append(w.deleted, target.ID)
	if w.failDeleteCount > 0 {
		w.failDeleteCount--
		return w.deleteErr
	}
	if w.inner == nil {
		return nil
	}
	return w.inner.DeleteAllForTarget(ctx, target)
}

func (w *trackingExtWriter) DeleteOrphanCartItemExtensions(ctx context.Context, cartID string, keepVariantIDs []string) error {
	keep := append([]string(nil), keepVariantIDs...)
	w.orphanPurges = append(w.orphanPurges, orphanExtensionPurge{cartID: cartID, keepVariantIDs: keep})
	if w.failDeleteCount > 0 {
		w.failDeleteCount--
		return w.deleteErr
	}
	if w.inner == nil {
		return nil
	}
	return w.inner.DeleteOrphanCartItemExtensions(ctx, cartID, keepVariantIDs)
}

func (w *trackingExtWriter) CopyTarget(ctx context.Context, from, to domainext.Target, updatedBy string) error {
	w.copied++
	if w.inner == nil {
		return nil
	}
	return w.inner.CopyTarget(ctx, from, to, updatedBy)
}

func (w *trackingExtWriter) ListByTarget(ctx context.Context, target domainext.Target) ([]domainext.Value, error) {
	if w.inner == nil {
		return nil, nil
	}
	return w.inner.ListByTarget(ctx, target)
}

func (w *trackingExtWriter) ListByCartItems(ctx context.Context, cartID string, variantIDs []string, includePrivate bool) (map[string][]domainext.Value, error) {
	if w.inner == nil {
		return map[string][]domainext.Value{}, nil
	}
	return w.inner.ListByCartItems(ctx, cartID, variantIDs, includePrivate)
}

func (w *trackingExtWriter) FieldOwner(code string) string {
	if w.inner == nil {
		return ""
	}
	return w.inner.FieldOwner(code)
}

func (w *trackingExtWriter) IsExclusiveString(code string) bool {
	if w.inner == nil {
		return false
	}
	return w.inner.IsExclusiveString(code)
}

func (w *trackingExtWriter) ReplaceTarget(ctx context.Context, target domainext.Target, values []domainext.Value) error {
	if w.inner == nil {
		return nil
	}
	return w.inner.ReplaceTarget(ctx, target, values)
}

func (w *trackingExtWriter) ListTargetIDsByTypeAndIDPrefix(ctx context.Context, targetType domainext.TargetType, idPrefix string) ([]string, error) {
	if w.inner == nil {
		return nil, nil
	}
	return w.inner.ListTargetIDsByTypeAndIDPrefix(ctx, targetType, idPrefix)
}

// failNReplaceWriter fails the next failN ReplaceTarget calls on live
// cart_item targets (not merge-backup writes), then delegates.
type failNReplaceWriter struct {
	*trackingExtWriter
	failN int
	calls int
}

func (w *failNReplaceWriter) ReplaceTarget(ctx context.Context, target domainext.Target, values []domainext.Value) error {
	w.calls++
	if target.Type == domainext.TargetCartItem && w.failN > 0 {
		w.failN--
		return errors.New("replace boom")
	}
	return w.trackingExtWriter.ReplaceTarget(ctx, target, values)
}

func setupCartExtensionService(t *testing.T) (*cartApp.Service, *memCartExtensionValueRepo) {
	t.Helper()
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register field: %v", err)
	}
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, nil)
	return svc, repo
}

type failExistingSaveCartRepo struct {
	*stubCartRepo
}

func (r *failExistingSaveCartRepo) Save(ctx context.Context, c *domainCart.Cart) error {
	if _, exists := r.carts[c.ID]; exists {
		return errors.New("disk full")
	}
	return r.stubCartRepo.Save(ctx, c)
}

func TestService_DiscardEmptyGuestCart_PurgesOrphanExtensions(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	extRepo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, extRepo)
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, nil)
	ctx := context.Background()

	c, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	// Orphan extension row with no cart line (AddItem now Saves before upsert,
	// so a failed Save never leaves extension leftovers — plant orphans directly).
	target := domainext.CartItemTarget(c.ID, "var-orphan")
	if _, err := values.UpsertBatch(ctx, target, []domainext.ValueInput{
		{FieldCode: "acme.gift.message", Value: "Hello"},
	}, "system", false); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	if len(extRepo.values) == 0 {
		t.Fatal("expected orphan extension row")
	}

	if err := svc.DiscardEmptyGuestCart(ctx, c.ID); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if got, _ := carts.FindByID(ctx, c.ID); got != nil {
		t.Fatal("empty guest cart should be deleted")
	}
	if len(extRepo.values) != 0 {
		t.Fatalf("extension leftovers = %d, want 0", len(extRepo.values))
	}
}

func TestService_AddItem_WithExtensions(t *testing.T) {
	svc, repo := setupCartExtensionService(t)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}

	got, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Hello"}},
		UpdatedBy:  "cust-1",
	})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(got.Items))
	}

	target := domainext.CartItemTarget(c.ID, "var-1")
	stored, err := repo.ListByTarget(ctx, target)
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "Hello" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestService_AddItem_BeforeHookSeesExtensionsAndActor(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	hookReg := hooks.NewRegistry(nil)
	var actorID, actorRole string
	if err := hookReg.Register(hooks.HookCartAddItemBefore, 100, "plugin.test", func(hctx *hooks.Context) error {
		raw, ok := hctx.Get("extensions")
		if !ok {
			t.Fatal("expected extensions in before-hook payload")
		}
		list, ok := raw.([]map[string]interface{})
		if !ok || len(list) != 1 || list[0]["field_code"] != "acme.gift.message" {
			t.Fatalf("extensions = %#v", raw)
		}
		if v, _ := hctx.Get("actor_user_id"); v != nil {
			actorID, _ = v.(string)
		}
		if v, _ := hctx.Get("actor_role"); v != nil {
			actorRole, _ = v.(string)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, hookReg)

	id, err := identity.NewIdentity("cust-1", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithIdentity(context.Background(), id)
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Hello"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if actorID != "cust-1" || actorRole != string(identity.RoleCustomer) {
		t.Fatalf("actor = %q/%q", actorID, actorRole)
	}
}

type failUpsertExtWriter struct {
	*trackingExtWriter
}

func (w *failUpsertExtWriter) UpsertBatch(context.Context, domainext.Target, []domainext.ValueInput, string, bool) ([]domainext.Value, error) {
	return nil, errors.New("upsert boom")
}

func (w *failUpsertExtWriter) UpsertBatchSelective(context.Context, domainext.Target, []domainext.ValueInput, string, bool, map[string]struct{}) ([]domainext.Value, error) {
	return nil, errors.New("upsert boom")
}

func TestService_AddItem_ExtensionUpsertFailureCompensatesCart(t *testing.T) {
	reg := extensionapp.NewRegistry()
	_ = reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	})
	inner := extensionapp.NewValueService(reg, newMemCartExtensionValueRepo())
	writer := &failUpsertExtWriter{trackingExtWriter: &trackingExtWriter{inner: inner}}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected upsert failure")
	}
	got, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("items = %d after compensate, want 0", len(got.Items))
	}
}

// versionCartRepo enforces optimistic version like postgres CartRepo.Save.
type versionCartRepo struct {
	*stubCartRepo
}

func (r *versionCartRepo) Save(_ context.Context, c *domainCart.Cart) error {
	existing, ok := r.carts[c.ID]
	if ok && existing.Version != c.Version {
		return apperror.Conflict("cart was modified concurrently")
	}
	clone := *c
	clone.Items = append([]domainCart.Item(nil), c.Items...)
	if ok {
		clone.Version = c.Version + 1
	} else {
		clone.Version = 1
	}
	c.Version = clone.Version
	r.carts[c.ID] = &clone
	return nil
}

func TestService_AddItem_ExtensionUpsertFailureRestoresAssignmentAndExistingQty(t *testing.T) {
	reg := extensionapp.NewRegistry()
	_ = reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	})
	inner := extensionapp.NewValueService(reg, newMemCartExtensionValueRepo())
	writer := &failUpsertExtWriter{trackingExtWriter: &trackingExtWriter{inner: inner}}
	carts := &versionCartRepo{stubCartRepo: newStubCartRepo()}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	// Seed line without extensions (upsert succeeds when no extensions).
	seeded, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if seeded.Items[0].Quantity != 2 {
		t.Fatalf("qty = %d", seeded.Items[0].Quantity)
	}

	parentCtx := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-new"})
	_, err = svc.AddItem(parentCtx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "x"}},
	})
	if err == nil {
		t.Fatal("expected upsert failure")
	}
	got, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Quantity != 2 {
		t.Fatalf("after compensate items=%+v, want qty 2", got.Items)
	}
	if got.Items[0].AssignmentParent() != "" {
		t.Fatalf("assignment parent = %q, want empty after compensate", got.Items[0].AssignmentParent())
	}
}

// TestService_AddItem_ExtensionUpsertFailureDoesNotRepriceSiblingLine
// covers: a failed extension upsert after Save must restore sibling lines'
// stored unit prices, not leave them at the recalculate/catalog price.
func TestService_AddItem_ExtensionUpsertFailureDoesNotRepriceSiblingLine(t *testing.T) {
	reg := extensionapp.NewRegistry()
	_ = reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	})
	inner := extensionapp.NewValueService(reg, newMemCartExtensionValueRepo())
	writer := &failUpsertExtWriter{trackingExtWriter: &trackingExtWriter{inner: inner}}
	carts := &versionCartRepo{stubCartRepo: newStubCartRepo()}
	prices := newStubPriceRepo()
	prices.set("var-a", "EUR", 1000)
	prices.set("var-b", "EUR", 2000)
	vr := mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-a": {ID: "var-a", ProductID: "prod-a"},
		"var-b": {ID: "var-b", ProductID: "prod-b"},
	}}
	pr := mapProductRepo{products: map[string]*catalog.Product{
		"prod-a": {ID: "prod-a", Status: catalog.StatusActive},
		"prod-b": {ID: "prod-b", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-a", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("seed var-a: %v", err)
	}
	// Price list for sibling A changes before adding B with a failing extension upsert.
	prices.set("var-a", "EUR", 1500)

	_, err = svc.AddItem(ctx, c.ID, "cust-1", "var-b", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "x"}},
	})
	if err == nil {
		t.Fatal("expected upsert failure")
	}
	got, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].VariantID != "var-a" {
		t.Fatalf("items after compensate = %+v, want only var-a", got.Items)
	}
	if got.Items[0].UnitPrice.Amount() != 1000 {
		t.Fatalf("sibling var-a unit price = %d, want 1000 (pre-recalculate)", got.Items[0].UnitPrice.Amount())
	}
}

// TestService_AddItem_ExtensionUpsertFailureDoesNotRepriceCompensatedLine
// covers the Track E review fix: compensateAddItemCart must restore the
// snapshotted unit price verbatim and must not call recalculate, which would
// re-run the pricing pipeline against the (possibly since-changed) catalog
// and silently reprice the compensated line.
func TestService_AddItem_ExtensionUpsertFailureDoesNotRepriceCompensatedLine(t *testing.T) {
	reg := extensionapp.NewRegistry()
	_ = reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	})
	inner := extensionapp.NewValueService(reg, newMemCartExtensionValueRepo())
	writer := &failUpsertExtWriter{trackingExtWriter: &trackingExtWriter{inner: inner}}
	carts := &versionCartRepo{stubCartRepo: newStubCartRepo()}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	// Seed the line at unit price 1000 (no extensions, so the upsert step is skipped).
	seeded, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if seeded.Items[0].UnitPrice.Amount() != 1000 {
		t.Fatalf("seed unit price = %d, want 1000", seeded.Items[0].UnitPrice.Amount())
	}

	// Price list changes before the next (failing) AddItem call.
	prices.set("var-1", "EUR", 9999)

	parentCtx := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-new"})
	_, err = svc.AddItem(parentCtx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "x"}},
	})
	if err == nil {
		t.Fatal("expected upsert failure")
	}
	got, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items after compensate = %+v, want 1", got.Items)
	}
	if got.Items[0].UnitPrice.Amount() != 1000 {
		t.Fatalf("unit price after compensate = %d, want 1000 (not repriced to the new 9999 catalog price)", got.Items[0].UnitPrice.Amount())
	}
}

func TestService_AddItem_BeforeHookShowsTrustedPluginOnlyValue(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.RegisterOwned(domainext.FieldDef{
		Code: "acme.secret", Label: "Secret", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Access: domainext.Access{PluginOnlyWrite: true},
	}, "plugin.test"); err != nil {
		t.Fatal(err)
	}
	values := extensionapp.NewValueService(reg, newMemCartExtensionValueRepo())
	hookReg := hooks.NewRegistry(nil)
	var sawValue interface{}
	var writeCodes []string
	_ = hookReg.Register(hooks.HookCartAddItemBefore, 100, "plugin.test", func(hctx *hooks.Context) error {
		raw, _ := hctx.Get("extensions")
		list, _ := raw.([]map[string]interface{})
		if len(list) == 1 {
			sawValue = list[0]["value"]
		}
		if codes, ok := hctx.Get("plugin_write_codes"); ok {
			writeCodes, _ = codes.([]string)
		}
		return nil
	})
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, hookReg)
	ctx := extensionapp.WithPluginWrite(context.Background(), "plugin.test", "acme.secret")
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.secret", Value: "classified"}},
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if sawValue != "classified" {
		t.Fatalf("value = %v, want classified when trusted owner", sawValue)
	}
	if len(writeCodes) != 1 || writeCodes[0] != "acme.secret" {
		t.Fatalf("plugin_write_codes = %v", writeCodes)
	}
}

// TestService_AddItem_BeforeHookOwnedPluginOnlyValueHiddenFromOtherRegistrant
// covers the Track E review fix: an owned plugin-only value must appear only
// on the owning registrant's hook payload, not on every handler for the
// same hook just because the write path trusted the field code.
func TestService_AddItem_BeforeHookOwnedPluginOnlyValueHiddenFromOtherRegistrant(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.RegisterOwned(domainext.FieldDef{
		Code: "acme.secret", Label: "Secret", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Access: domainext.Access{PluginOnlyWrite: true},
	}, "acme"); err != nil {
		t.Fatal(err)
	}
	values := extensionapp.NewValueService(reg, newMemCartExtensionValueRepo())
	hookReg := hooks.NewRegistry(nil)

	var ownerSawValue interface{}
	var ownerSawFieldCode interface{}
	_ = hookReg.Register(hooks.HookCartAddItemBefore, 100, "acme", func(hctx *hooks.Context) error {
		raw, _ := hctx.Get("extensions")
		list, _ := raw.([]map[string]interface{})
		if len(list) == 1 {
			ownerSawFieldCode = list[0]["field_code"]
			ownerSawValue = list[0]["value"]
		}
		return nil
	})
	var otherSawValue interface{}
	var otherHadValueKey bool
	var otherSawFieldCode interface{}
	var otherWriteCodes []string
	_ = hookReg.Register(hooks.HookCartAddItemBefore, 200, "other.plugin", func(hctx *hooks.Context) error {
		raw, _ := hctx.Get("extensions")
		list, _ := raw.([]map[string]interface{})
		if len(list) == 1 {
			otherSawFieldCode = list[0]["field_code"]
			otherSawValue, otherHadValueKey = list[0]["value"]
		}
		if codes, ok := hctx.Get("plugin_write_codes"); ok {
			otherWriteCodes, _ = codes.([]string)
		}
		return nil
	})

	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, hookReg)
	ctx := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.secret")
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.secret", Value: "classified"}},
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if ownerSawFieldCode != "acme.secret" || ownerSawValue != "classified" {
		t.Fatalf("owner saw field_code=%v value=%v, want acme.secret/classified", ownerSawFieldCode, ownerSawValue)
	}
	if otherSawFieldCode != "acme.secret" {
		t.Fatalf("other registrant field_code = %v, want acme.secret (always present)", otherSawFieldCode)
	}
	if otherHadValueKey {
		t.Fatalf("other registrant saw value %v, want redacted (owner-only field)", otherSawValue)
	}
	if len(otherWriteCodes) != 0 {
		t.Fatalf("other registrant plugin_write_codes = %v, want empty (owner-only)", otherWriteCodes)
	}
}

func TestService_AddItem_InvalidExtensionRejected(t *testing.T) {
	svc, _ := setupCartExtensionService(t)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}

	_, err = svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "missing.field", Value: "x"}},
		UpdatedBy:  "cust-1",
	})
	if !errors.Is(err, domainext.ErrUnknownFieldCode) {
		t.Fatalf("AddItem err = %v, want ErrUnknownFieldCode", err)
	}
}

func TestService_UpdateItemQuantity_PreservesExtensions(t *testing.T) {
	svc, repo := setupCartExtensionService(t)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep me"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	got, err := svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 3)
	if err != nil {
		t.Fatalf("UpdateItemQuantity: %v", err)
	}
	if got.Items[0].Quantity != 3 {
		t.Fatalf("quantity = %d, want 3", got.Items[0].Quantity)
	}

	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(c.ID, "var-1"))
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "Keep me" {
		t.Fatalf("stored after update = %+v", stored)
	}
}

func TestService_RemoveItem_DeletesExtensions(t *testing.T) {
	svc, repo := setupCartExtensionService(t)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Remove me"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if _, err := svc.RemoveItem(ctx, c.ID, "cust-1", "var-1"); err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}

	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(c.ID, "var-1"))
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("stored after remove = %+v, want empty", stored)
	}
}

func TestService_ClaimGuestCart_MergeCopiesAndCleansExtensions(t *testing.T) {
	svc, repo := setupCartExtensionService(t)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}

	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Merged"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}

	merged, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart: %v", err)
	}
	if merged.ID != customerCart.ID {
		t.Fatalf("merged cart id = %q, want %q", merged.ID, customerCart.ID)
	}

	customerTarget := domainext.CartItemTarget(customerCart.ID, "var-1")
	stored, err := repo.ListByTarget(ctx, customerTarget)
	if err != nil {
		t.Fatalf("ListByTarget customer: %v", err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "Merged" {
		t.Fatalf("customer stored = %+v", stored)
	}

	guestTarget := domainext.CartItemTarget(guestCart.ID, "var-1")
	guestStored, err := repo.ListByTarget(ctx, guestTarget)
	if err != nil {
		t.Fatalf("ListByTarget guest: %v", err)
	}
	if len(guestStored) != 0 {
		t.Fatalf("guest stored after merge = %+v, want empty", guestStored)
	}
}

// failNthCopyWriter fails CopyTarget on the Nth call (1-based), then succeeds.
type failNthCopyWriter struct {
	*trackingExtWriter
	failOn int
	calls  int
}

func (w *failNthCopyWriter) CopyTarget(ctx context.Context, from, to domainext.Target, updatedBy string) error {
	w.calls++
	if w.failOn > 0 && w.calls == w.failOn {
		return errors.New("copy boom")
	}
	return w.trackingExtWriter.CopyTarget(ctx, from, to, updatedBy)
}

func TestService_ClaimGuestCart_CopyFailureKeepsGuestRowsAndRetryFinishes(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	writer := &failNthCopyWriter{trackingExtWriter: &trackingExtWriter{inner: inner}, failOn: 2}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	// Permissive catalog only has var-1 by default — use map repos for two variants.
	vr = mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-1": {ID: "var-1", ProductID: "prod-1"},
		"var-2": {ID: "var-2", ProductID: "prod-2"},
	}}
	pr = mapProductRepo{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	// var-1 already has a customer value before the merge — the first
	// CopyTarget call (var-1) will succeed and overwrite it; when the second
	// call (var-2) then fails, that overwrite must be rolled back since the
	// merge never became durable (MergedGuestID never saved).
	if _, err := svc.AddItem(ctx, customerCart.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep me"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem customer var-1: %v", err)
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		id  string
		msg string
	}{
		{"var-1", "Note-1"},
		{"var-2", "Note-2"},
	} {
		if _, err := svc.AddItem(ctx, guestCart.ID, "", v.id, 1, cartApp.AddItemOptions{
			Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: v.msg}},
			UpdatedBy:  "guest",
		}); err != nil {
			t.Fatalf("AddItem %s: %v", v.id, err)
		}
	}

	_, err = svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1")
	if err == nil {
		t.Fatal("expected copy failure on second variant")
	}

	// Guest rows must still exist (merge not durable — MergedGuestID not saved).
	for _, id := range []string{"var-1", "var-2"} {
		got, err := repo.ListByTarget(ctx, domainext.CartItemTarget(guestCart.ID, id))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("guest %s extensions = %d, want 1 after failed claim", id, len(got))
		}
	}
	storedCustomer, _ := carts.FindByID(ctx, customerCart.ID)
	if storedCustomer != nil && storedCustomer.MergedGuestID != "" {
		t.Fatalf("MergedGuestID = %q after failed copy, want empty", storedCustomer.MergedGuestID)
	}
	// The var-1 CopyTarget that already ran before var-2 failed must have
	// been rolled back to the pre-merge customer value.
	customerVar1, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(customerVar1) != 1 || customerVar1[0].Payload.StringValue == nil || *customerVar1[0].Payload.StringValue != "Keep me" {
		t.Fatalf("customer var-1 after partial copy failure = %+v, want restored %q", customerVar1, "Keep me")
	}

	// Retry with copies succeeding.
	writer.failOn = 0
	writer.calls = 0
	merged, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1")
	if err != nil {
		t.Fatalf("retry ClaimGuestCart: %v", err)
	}
	if merged.ID != customerCart.ID {
		t.Fatalf("merged id = %q", merged.ID)
	}
	for _, v := range []struct {
		id  string
		msg string
	}{
		{"var-1", "Note-1"},
		{"var-2", "Note-2"},
	} {
		got, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, v.id))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Payload.StringValue == nil || *got[0].Payload.StringValue != v.msg {
			t.Fatalf("customer %s = %+v, want %q", v.id, got, v.msg)
		}
		guestGot, _ := repo.ListByTarget(ctx, domainext.CartItemTarget(guestCart.ID, v.id))
		if len(guestGot) != 0 {
			t.Fatalf("guest %s still has extensions after successful retry", v.id)
		}
	}
}

// TestService_ClaimGuestCart_ReplaceTargetFailKeepsSnapshotForRetryClaim
// covers: CopyTarget commits, ReplaceTarget restore fails (after retries),
// a second claim on a *new* Service (simulating another process) must apply
// the durable merge backup so the stored value is still "Keep me".
func TestService_ClaimGuestCart_ReplaceTargetFailKeepsSnapshotForRetryClaim(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	// Fail enough cart_item ReplaceTarget calls to exhaust mergeRestoreAttempts
	// on the first claim's restore; merge-backup writes still succeed.
	writer := &failNReplaceWriter{trackingExtWriter: &trackingExtWriter{inner: inner}, failN: 3}
	carts := &failNthSaveCartRepo{stubCartRepo: newStubCartRepo(), saveErr: errors.New("save conflict")}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, customerCart.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep me"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem customer: %v", err)
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Overwrite"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}

	// First claim: CopyTarget commits, Save fails, ReplaceTarget fails all retries.
	carts.failOn = carts.saveN + 1
	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected save conflict + restore failure on first claim")
	}
	afterFirst, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(afterFirst) != 1 || afterFirst[0].Payload.StringValue == nil || *afterFirst[0].Payload.StringValue != "Overwrite" {
		t.Fatalf("after failed restore = %+v, want guest Overwrite still stored", afterFirst)
	}

	// Second claim on a fresh Service sharing the same repos — durable backup
	// must restore Keep me before any new snapshot/copy.
	writer2 := &trackingExtWriter{inner: extensionapp.NewValueService(reg, repo)}
	svc2 := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer2, nil)
	carts.failOn = carts.saveN + 1
	if _, err := svc2.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected save conflict on second claim")
	}
	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "Keep me" {
		t.Fatalf("after retry claim on new Service = %+v, want restored %q", stored, "Keep me")
	}
}

// barrierSaveCartRepo, once armed, blocks merge Saves (MergedGuestID set)
// until both callers have entered, then lets the first commit and returns a
// version conflict for the second. Setup Saves pass through unblocked.
type barrierSaveCartRepo struct {
	*versionCartRepo
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	armed   bool
	merges  int
}

func (r *barrierSaveCartRepo) Save(ctx context.Context, c *domainCart.Cart) error {
	r.mu.Lock()
	armed := r.armed
	isMerge := c != nil && c.MergedGuestID != ""
	var n int
	if armed && isMerge {
		r.merges++
		n = r.merges
	}
	r.mu.Unlock()
	if !armed || !isMerge {
		return r.versionCartRepo.Save(ctx, c)
	}
	r.entered <- struct{}{}
	<-r.release
	r.mu.Lock()
	defer r.mu.Unlock()
	if n == 1 {
		return r.versionCartRepo.Save(ctx, c)
	}
	return apperror.Conflict("cart was modified concurrently")
}

// TestService_ClaimGuestCart_ConcurrentSaveConflictKeepsGuestExtensions
// covers: two overlapping merges both CopyTarget; one Save commits
// MergedGuestID; the loser must not ReplaceTarget the pre-merge snapshot
// back over the winner's guest extension values.
func TestService_ClaimGuestCart_ConcurrentSaveConflictKeepsGuestExtensions(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	writer := &trackingExtWriter{inner: inner}
	base := &versionCartRepo{stubCartRepo: newStubCartRepo()}
	carts := &barrierSaveCartRepo{
		versionCartRepo: base,
		entered:         make(chan struct{}, 2),
		release:         make(chan struct{}),
	}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, customerCart.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "customer"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem customer: %v", err)
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "guest"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}

	carts.mu.Lock()
	carts.armed = true
	carts.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1")
			errs <- err
		}()
	}
	<-carts.entered
	<-carts.entered
	close(carts.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ClaimGuestCart: %v", err)
		}
	}

	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "guest" {
		t.Fatalf("after concurrent claim = %+v, want guest value", stored)
	}
	got, err := carts.FindByID(ctx, customerCart.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.MergedGuestID != guestCart.ID {
		t.Fatalf("MergedGuestID = %q, want %q", got.MergedGuestID, guestCart.ID)
	}
}

// TestService_ClaimGuestCart_SaveConflictAfterCopyRestoresCustomerExtensions
// covers the Track E review BLOCKER: CopyTarget commits independently of the
// cart Save that follows it, so a Save failure after a successful copy must
// restore the customer cart's pre-merge extension rows instead of leaving
// them silently overwritten by the guest's value for a merge that never
// became durable.
func TestService_ClaimGuestCart_SaveConflictAfterCopyRestoresCustomerExtensions(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	writer := &trackingExtWriter{inner: inner}
	carts := &failNthSaveCartRepo{stubCartRepo: newStubCartRepo(), saveErr: errors.New("save conflict")}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	// Customer cart already has an extension value on var-1 before the merge.
	if _, err := svc.AddItem(ctx, customerCart.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep me"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem customer: %v", err)
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Overwrite"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}

	// Fail the next Save call: the merged-cart Save inside ClaimGuestCart.
	// CopyTarget (var-1) runs and succeeds before this Save, per the merge
	// order, so the bug under test is real without this line reaching a
	// code path that never copies anything.
	carts.failOn = carts.saveN + 1

	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected save conflict after copy")
	}

	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "Keep me" {
		t.Fatalf("customer var-1 after save conflict = %+v, want restored %q", stored, "Keep me")
	}

	storedCustomerCart, err := carts.FindByID(ctx, customerCart.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedCustomerCart == nil || storedCustomerCart.MergedGuestID != "" {
		t.Fatalf("MergedGuestID = %q after save conflict, want empty", storedCustomerCart.MergedGuestID)
	}

	guestStored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(guestCart.ID, "var-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(guestStored) != 1 {
		t.Fatalf("guest var-1 extensions = %d, want 1 (guest rows still present for retry)", len(guestStored))
	}
}

func TestService_ClaimGuestCart_LaterAssignmentConflict_DoesNotOverwriteExtensions(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register field: %v", err)
	}
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	vr := mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-1": {ID: "var-1", ProductID: "prod-1"},
		"var-2": {ID: "var-2", ProductID: "prod-2"},
	}}
	pr := mapProductRepo{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, nil)
	ctx := context.Background()

	customer, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if _, err := svc.AddItem(ctx, customer.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep me"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem customer var-1: %v", err)
	}
	parentA := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-a"})
	if _, err := svc.AddItem(parentA, customer.ID, "cust-1", "var-2", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem customer var-2: %v", err)
	}
	// Shared variant is first so a late conflict used to copy its extensions
	// before the merge aborted and before the customer cart was saved.
	if _, err := svc.AddItem(ctx, guest.ID, "", "var-1", 2, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Overwrite"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem guest var-1: %v", err)
	}
	parentB := catalog.WithAssignmentContext(ctx, catalog.AssignmentContext{ParentProductID: "card-b"})
	if _, err := svc.AddItem(parentB, guest.ID, "", "var-2", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem guest var-2: %v", err)
	}

	_, err = svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), "different linked parent assignment") {
		t.Fatalf("err = %v, want conflicting assignment", err)
	}

	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customer.ID, "var-1"))
	if err != nil {
		t.Fatalf("ListByTarget customer: %v", err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "Keep me" {
		t.Fatalf("customer extension = %+v, want Keep me", stored)
	}
	saved, err := carts.FindByID(ctx, customer.ID)
	if err != nil || saved == nil {
		t.Fatalf("customer cart: %v", err)
	}
	qty := map[string]int{}
	parent := map[string]string{}
	for _, item := range saved.Items {
		qty[item.VariantID] = item.Quantity
		parent[item.VariantID] = item.AssignmentParent()
	}
	if qty["var-1"] != 1 || qty["var-2"] != 1 || parent["var-2"] != "card-a" {
		t.Fatalf("customer items = %+v, want var-1 qty 1 and var-2 qty 1 parent card-a", saved.Items)
	}
}

func TestService_ClaimGuestCart_AssignDeleteFailsAfterSave_RetryPurgesOrphans(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register field: %v", err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	ext := &trackingExtWriter{inner: inner, deleteErr: errors.New("delete failed"), failDeleteCount: 1}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	vr := mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-1": {ID: "var-1", ProductID: "prod-1"},
		"var-2": {ID: "var-2", ProductID: "prod-2"},
	}}
	pr := mapProductRepo{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	for _, spec := range []struct {
		variantID string
		message   string
	}{
		{"var-1", "Keep"},
		{"var-2", "Orphan"},
	} {
		if _, err := svc.AddItem(ctx, guestCart.ID, "", spec.variantID, 1, cartApp.AddItemOptions{
			Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: spec.message}},
			UpdatedBy:  "guest",
		}); err != nil {
			t.Fatalf("AddItem %s: %v", spec.variantID, err)
		}
	}
	pr.products["prod-2"] = &catalog.Product{ID: "prod-2", Status: catalog.StatusArchived}

	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected extension cleanup failure on first claim")
	}
	claimed, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1")
	if err != nil {
		t.Fatalf("retry ClaimGuestCart: %v", err)
	}
	if claimed.CustomerID != "cust-1" {
		t.Fatalf("customer id = %q, want cust-1", claimed.CustomerID)
	}
	orphan, err := repo.ListByTarget(ctx, domainext.CartItemTarget(guestCart.ID, "var-2"))
	if err != nil {
		t.Fatalf("ListByTarget orphan: %v", err)
	}
	if len(orphan) != 0 {
		t.Fatalf("orphan extension after retry = %+v, want empty", orphan)
	}
	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(guestCart.ID, "var-1"))
	if err != nil {
		t.Fatalf("ListByTarget kept: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored after retry = %+v, want one row", stored)
	}
}

// TestService_ClaimGuestCart_CopyFailOnFirstDoesNotWipeUncopiedVariant
// covers the restore-scope blocker: a CopyTarget failure must restore only
// variants that already copied. Restoring an uncopied target via ReplaceTarget
// (especially delete-all-then-upsert) can wipe a customer "Keep me" that the
// failed merge never touched.
func TestService_ClaimGuestCart_CopyFailOnFirstDoesNotWipeUncopiedVariant(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	writer := &failNthCopyWriter{trackingExtWriter: &trackingExtWriter{inner: inner}, failOn: 1}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	vr := mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-1": {ID: "var-1", ProductID: "prod-1"},
		"var-2": {ID: "var-2", ProductID: "prod-2"},
	}}
	pr := mapProductRepo{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), writer, nil)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(ctx, customerCart.ID, "cust-1", id, 1, cartApp.AddItemOptions{
			Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep me"}},
			UpdatedBy:  "cust-1",
		}); err != nil {
			t.Fatalf("AddItem customer %s: %v", id, err)
		}
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(ctx, guestCart.ID, "", id, 1, cartApp.AddItemOptions{
			Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Overwrite"}},
			UpdatedBy:  "guest",
		}); err != nil {
			t.Fatalf("AddItem guest %s: %v", id, err)
		}
	}

	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected copy failure on first variant")
	}

	for _, id := range []string{"var-1", "var-2"} {
		got, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, id))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Payload.StringValue == nil || *got[0].Payload.StringValue != "Keep me" {
			t.Fatalf("customer %s after first-copy failure = %+v, want Keep me", id, got)
		}
	}
}

// TestService_ClaimGuestCart_HealRetryDoesNotOverwriteDroppedOverlap
// covers heal after durable MergedGuestID: a guest line dropped by
// unclaimableGuestLines stays on the guest cart and may share a variant
// with a pre-existing customer line. Heal must not copy that dropped
// guest's extensions onto the customer line.
func TestService_ClaimGuestCart_HealRetryDoesNotOverwriteDroppedOverlap(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	ext := &trackingExtWriter{inner: inner, deleteErr: errors.New("delete failed"), failDeleteCount: 1}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	vr := mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-1": {ID: "var-1", ProductID: "prod-1"},
		"var-2": {ID: "var-2", ProductID: "prod-2"},
	}}
	pr := mapProductRepo{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, customerCart.ID, "cust-1", "var-2", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep me"}},
		UpdatedBy:  "cust-1",
	}); err != nil {
		t.Fatalf("AddItem customer var-2: %v", err)
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "FromGuest"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem guest var-1: %v", err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-2", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Overwrite"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem guest var-2: %v", err)
	}
	// Drop guest var-2 (archived product); customer keeps its own var-2 line.
	pr.products["prod-2"] = &catalog.Product{ID: "prod-2", Status: catalog.StatusArchived}

	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected cleanup failure on first claim")
	}
	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err != nil {
		t.Fatalf("heal retry: %v", err)
	}

	kept, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Payload.StringValue == nil || *kept[0].Payload.StringValue != "Keep me" {
		t.Fatalf("customer var-2 after heal = %+v, want Keep me (dropped guest must not overwrite)", kept)
	}
}

// TestService_ClaimGuestCart_HealRetryDoesNotCopyDroppedLineExtensions
// covers the Track E review fix: finishMergedGuestExtensionCopies (the heal
// path used when MergedGuestID is already durable but cleanup is retried)
// must only copy guest variants that exist on the customer cart. A line
// dropped by unclaimableGuestLines during the original merge was never added
// to the customer cart, so retrying cleanup must not resurrect an orphaned
// extension row for it on the customer cart.
func TestService_ClaimGuestCart_HealRetryDoesNotCopyDroppedLineExtensions(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register field: %v", err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	ext := &trackingExtWriter{inner: inner, deleteErr: errors.New("delete failed"), failDeleteCount: 1}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	vr := mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-1": {ID: "var-1", ProductID: "prod-1"},
		"var-2": {ID: "var-2", ProductID: "prod-2"},
	}}
	pr := mapProductRepo{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Keep"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem var-1: %v", err)
	}
	if _, err := svc.AddItem(ctx, guestCart.ID, "", "var-2", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Stale"}},
		UpdatedBy:  "guest",
	}); err != nil {
		t.Fatalf("AddItem var-2: %v", err)
	}

	// var-2's product is archived before the merge, so unclaimableGuestLines
	// drops it: it never becomes a customer cart line, and its extensions
	// are never copied during the initial merge.
	pr.products["prod-2"] = &catalog.Product{ID: "prod-2", Status: catalog.StatusArchived}

	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected cleanup failure on first claim")
	}

	// Retry: MergedGuestID is already durable, so this goes through the heal
	// path (finishMergedGuestExtensionCopies), which must not resurrect
	// var-2's extension on the customer cart.
	claimed, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1")
	if err != nil {
		t.Fatalf("retry ClaimGuestCart: %v", err)
	}
	if claimed.ID != customerCart.ID {
		t.Fatalf("claimed id = %q, want %q", claimed.ID, customerCart.ID)
	}
	if len(claimed.Items) != 1 || claimed.Items[0].VariantID != "var-1" {
		t.Fatalf("items after claim = %+v, want only var-1", claimed.Items)
	}

	orphan, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(orphan) != 0 {
		t.Fatalf("customer var-2 extensions = %+v, want none (var-2 was dropped from the merge)", orphan)
	}

	kept, err := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Payload.StringValue == nil || *kept[0].Payload.StringValue != "Keep" {
		t.Fatalf("customer var-1 extensions = %+v, want Keep", kept)
	}
}

func TestService_ClaimGuestCart_AssignPartialCleanupFailure_ReAddDroppedVariantWithoutExtensions(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register field: %v", err)
	}
	repo := newMemCartExtensionValueRepo()
	inner := extensionapp.NewValueService(reg, repo)
	ext := &trackingExtWriter{inner: inner, deleteErr: errors.New("delete failed"), failDeleteCount: 1}
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	vr := mapVariantRepo{variants: map[string]*catalog.Variant{
		"var-1": {ID: "var-1", ProductID: "prod-1"},
		"var-2": {ID: "var-2", ProductID: "prod-2"},
	}}
	pr := mapProductRepo{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive},
	}}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	for _, spec := range []struct {
		variantID string
		message   string
	}{
		{"var-1", "Keep"},
		{"var-2", "Stale"},
	} {
		if _, err := svc.AddItem(ctx, guestCart.ID, "", spec.variantID, 1, cartApp.AddItemOptions{
			Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: spec.message}},
			UpdatedBy:  "guest",
		}); err != nil {
			t.Fatalf("AddItem %s: %v", spec.variantID, err)
		}
	}
	pr.products["prod-2"] = &catalog.Product{ID: "prod-2", Status: catalog.StatusArchived}

	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err == nil {
		t.Fatal("expected cleanup failure on first assign claim")
	}
	claimed, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1")
	if err != nil {
		t.Fatalf("retry ClaimGuestCart: %v", err)
	}
	if len(claimed.Items) != 1 || claimed.Items[0].VariantID != "var-1" {
		t.Fatalf("items after claim = %+v, want only var-1", claimed.Items)
	}
	pr.products["prod-2"] = &catalog.Product{ID: "prod-2", Status: catalog.StatusActive}
	if _, err := svc.AddItem(ctx, claimed.ID, "cust-1", "var-2", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("re-add var-2: %v", err)
	}
	stored, err := repo.ListByTarget(ctx, domainext.CartItemTarget(claimed.ID, "var-2"))
	if err != nil {
		t.Fatalf("ListByTarget var-2: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("stale extension resurfaced = %+v, want empty", stored)
	}
}

type mapVariantRepo struct {
	variants map[string]*catalog.Variant
}

func (r mapVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return r.variants[id], nil
}

func (mapVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }

func (mapVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}

func (mapVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}

func (mapVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}

func (mapVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }

func (mapVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type mapProductRepo struct {
	products map[string]*catalog.Product
}

func (r mapProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return r.products[id], nil
}

func (mapProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }

func (mapProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}

func (mapProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}

func (mapProductRepo) Create(context.Context, *catalog.Product) error { return nil }

func (mapProductRepo) Update(context.Context, *catalog.Product) error { return nil }

// countingBatchVariants implements FindByIDs (postgres-style) and counts
// singular vs batch reads so tax-class recalculate can prove batching.
type countingBatchVariants struct {
	mapVariantRepo
	findByIDCalls  int
	findByIDsCalls int
}

func (r *countingBatchVariants) FindByID(ctx context.Context, id string) (*catalog.Variant, error) {
	r.findByIDCalls++
	return r.mapVariantRepo.FindByID(ctx, id)
}

func (r *countingBatchVariants) FindByIDs(_ context.Context, ids []string) (map[string]*catalog.Variant, error) {
	r.findByIDsCalls++
	out := make(map[string]*catalog.Variant, len(ids))
	for _, id := range ids {
		if v := r.variants[id]; v != nil {
			out[id] = v
		}
	}
	return out, nil
}

type countingBatchProducts struct {
	mapProductRepo
	findByIDCalls  int
	listByIDsCalls int
}

func (r *countingBatchProducts) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	r.findByIDCalls++
	return r.mapProductRepo.FindByID(ctx, id)
}

func (r *countingBatchProducts) ListByIDs(_ context.Context, ids []string) (map[string]*catalog.Product, error) {
	r.listByIDsCalls++
	out := make(map[string]*catalog.Product, len(ids))
	for _, id := range ids {
		if p := r.products[id]; p != nil {
			out[id] = p
		}
	}
	return out, nil
}

func setupCartPluginOnlyService(t *testing.T) (*cartApp.Service, *memCartExtensionValueRepo) {
	t.Helper()
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.design",
		Label:       "Design",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
		Access:      domainext.Access{PluginOnlyWrite: true},
	}); err != nil {
		t.Fatalf("register field: %v", err)
	}
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(newStubCartRepo(), prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, nil)
	return svc, repo
}

func TestService_AddItem_PluginOnlyWrite_UntrustedForbidden(t *testing.T) {
	svc, repo := setupCartPluginOnlyService(t)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.design", Value: "d1"}},
		UpdatedBy:  "cust-1",
	})
	if !errors.Is(err, domainext.ErrForbiddenPluginOnlyField) {
		t.Fatalf("AddItem err = %v, want ErrForbiddenPluginOnlyField", err)
	}
	stored, _ := repo.ListByTarget(ctx, domainext.CartItemTarget(c.ID, "var-1"))
	if len(stored) != 0 {
		t.Fatalf("stored = %+v, want none", stored)
	}
}

func TestService_PluginOnlyWrite_TrustedAddRemoveAndClaimMerge(t *testing.T) {
	svc, repo := setupCartPluginOnlyService(t)
	ctx := context.Background()
	trusted := extensionapp.WithPluginWrite(ctx, "acme", "acme.design")

	customerCart, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	guestCart, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(trusted, guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.design", Value: "d1"}},
		UpdatedBy:  "plugin:acme",
	}); err != nil {
		t.Fatalf("trusted AddItem: %v", err)
	}

	// Core claim/merge runs without plugin context and must still copy the field.
	if _, err := svc.ClaimGuestCart(ctx, guestCart.ID, "cust-1"); err != nil {
		t.Fatalf("ClaimGuestCart: %v", err)
	}
	stored, _ := repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if len(stored) != 1 {
		t.Fatalf("customer stored = %+v, want 1", stored)
	}

	// Removing the line deletes the row without plugin context.
	if _, err := svc.RemoveItem(ctx, customerCart.ID, "cust-1", "var-1"); err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	stored, _ = repo.ListByTarget(ctx, domainext.CartItemTarget(customerCart.ID, "var-1"))
	if len(stored) != 0 {
		t.Fatalf("after remove = %+v, want none", stored)
	}
}

func TestService_AddItem_TrustDoesNotLeakToEventHandlers(t *testing.T) {
	reg := extensionapp.NewRegistry()
	for _, code := range []string{"acme.design", "acme.other"} {
		if err := reg.Register(domainext.FieldDef{
			Code: code, Label: code, Type: domainext.FieldTypeString,
			Scope: domainext.TargetCartItem, StorageMode: domainext.StorageSnapshot,
			Access: domainext.Access{PluginOnlyWrite: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	repo := newMemCartExtensionValueRepo()
	values := extensionapp.NewValueService(reg, repo)
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	bus := testBus()
	var handlerErr error
	var ran bool
	bus.On(domainCart.EventItemAdded, func(ctx context.Context, _ event.Event) error {
		ran = true
		_, handlerErr = values.UpsertBatch(ctx, domainext.CartItemTarget("any", "var-1"),
			[]domainext.ValueInput{{FieldCode: "acme.design", Value: "stolen"}}, "handler", false)
		return nil
	})
	svc := cartApp.NewService(newStubCartRepo(), prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), bus, values, nil)
	ctx := context.Background()
	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	trusted := extensionapp.WithPluginWrite(ctx, "acme", "acme.design")
	if _, err := svc.AddItem(trusted, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.design", Value: "d1"}},
		UpdatedBy:  "plugin:acme",
	}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if !ran {
		t.Fatal("event handler did not run")
	}
	if !errors.Is(handlerErr, domainext.ErrForbiddenPluginOnlyField) {
		t.Fatalf("handler err = %v, want ErrForbiddenPluginOnlyField (trust must not leak)", handlerErr)
	}
}
