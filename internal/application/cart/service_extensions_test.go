package cart_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
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
	carts := &failExistingSaveCartRepo{stubCartRepo: newStubCartRepo()}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	vr, pr := cartApp.PermissiveCatalogRepos()
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), testPipeline(prices), testLogger(), testBus(), values, nil)
	ctx := context.Background()

	c, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddItem(ctx, c.ID, "", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "acme.gift.message", Value: "Hello"}},
		UpdatedBy:  "system",
	})
	if err == nil {
		t.Fatal("expected AddItem save to fail")
	}
	if len(extRepo.values) == 0 {
		t.Fatal("expected extension row written before Save failed")
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
