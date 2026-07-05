package cart_test

import (
	"context"
	"errors"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
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
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus(), values)
	return svc, repo
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
