package checkout_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type memSnapshotValueRepo struct {
	values map[string]domainext.Value
}

func snapshotKey(target domainext.Target, fieldCode string) string {
	return string(target.Type) + ":" + target.ID + ":" + fieldCode
}

func newMemSnapshotValueRepo() *memSnapshotValueRepo {
	return &memSnapshotValueRepo{values: make(map[string]domainext.Value)}
}

func (m *memSnapshotValueRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, value := range m.values {
		if value.TargetType == target.Type && value.TargetID == target.ID {
			out = append(out, value)
		}
	}
	return out, nil
}

func (m *memSnapshotValueRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
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

func (m *memSnapshotValueRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[snapshotKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)] = value
	return nil
}

func (m *memSnapshotValueRepo) UpsertBatch(_ context.Context, values []domainext.Value) error {
	for _, value := range values {
		if err := m.Upsert(context.Background(), value); err != nil {
			return err
		}
	}
	return nil
}

func (m *memSnapshotValueRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := snapshotKey(target, fieldCode)
	if _, ok := m.values[key]; !ok {
		return apperror.NotFound("extension value not found")
	}
	delete(m.values, key)
	return nil
}

func TestCreateOrderStep_SnapshotsCartItemExtensions(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newMemSnapshotValueRepo()
	values := extensionapp.NewValueService(reg, extRepo)

	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, values)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	cartTarget := domainext.CartItemTarget(cctx.Cart.ID, "v1")
	if _, err := values.UpsertBatch(context.Background(), cartTarget, []domainext.ValueInput{
		{FieldCode: "acme.gift.message", Value: "Ship fast"},
	}, "cust-1", false); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cctx.Order == nil {
		t.Fatal("expected order on context")
	}

	orderTarget := domainext.OrderItemTarget(cctx.Order.ID, "v1")
	stored, err := extRepo.ListByTarget(context.Background(), orderTarget)
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "Ship fast" {
		t.Fatalf("order extensions = %+v", stored)
	}
}
