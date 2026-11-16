package checkout_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/internal/platform/requestctx"
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

func (m *memSnapshotValueRepo) UpsertBatchSelective(ctx context.Context, values []domainext.Value, exclusiveFieldCodes map[string]struct{}) error {
	return m.UpsertBatch(ctx, values)
}

func (m *memSnapshotValueRepo) ListTargetIDsByTypeAndIDPrefix(_ context.Context, targetType domainext.TargetType, idPrefix string) ([]string, error) {
	prefix := strings.TrimSuffix(idPrefix, "%")
	seen := make(map[string]struct{})
	var out []string
	for _, value := range m.values {
		if value.TargetType != targetType || !strings.HasPrefix(value.TargetID, prefix) {
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

func (m *memSnapshotValueRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := snapshotKey(target, fieldCode)
	if _, ok := m.values[key]; !ok {
		return apperror.NotFound("extension value not found")
	}
	delete(m.values, key)
	return nil
}

func TestWorkflow_SnapshotsPluginOnlyCartItemExtensionsWithoutInheritedTrust(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.design",
		Label:       "Design",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
		Access:      domainext.Access{PluginOnlyWrite: true},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newMemSnapshotValueRepo()
	values := extensionapp.NewValueService(reg, extRepo)
	probe := &snapshotCtxProbe{inner: values}

	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, probe, idleProducts(), idleComponents(), idlePrices())
	wf := checkout.NewWorkflow([]checkout.Step{step}, testBus(t), testLogger())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	cartTarget := domainext.CartItemTarget(cctx.Cart.ID, "v1")
	seed := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.design")
	if _, err := values.UpsertBatch(seed, cartTarget, []domainext.ValueInput{
		{FieldCode: "acme.design", Value: "d1"},
	}, "plugin:acme", false); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	// Production path: Workflow.Execute strips trust; CreateOrderStep then uses
	// detachedTimeout (WithoutCancel), which keeps values from its parent — so
	// the parent must already be stripped or the grant would survive into Snapshot.
	// Request id must survive Strip + WithoutCancel; context.Background() would not.
	parent := requestctx.WithRequestID(context.Background(), "req-snap-1")
	trusted := extensionapp.WithPluginWrite(parent, "acme", "acme.design")
	if err := wf.Execute(trusted, cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cctx.Order == nil {
		t.Fatal("expected order on context")
	}
	if probe.saw == nil {
		t.Fatal("SnapshotCartItemToOrderItem was not called")
	}
	if _, ok := pluginwrite.For(probe.saw, "acme.design"); ok {
		t.Fatal("snapshot ctx must not inherit the Workflow caller's plugin-write grant")
	}
	if got := requestctx.RequestID(probe.saw); got != "req-snap-1" {
		t.Fatalf("snapshot RequestID = %q, want req-snap-1 (WithoutCancel must keep parent values)", got)
	}
	orderTarget := domainext.OrderItemTarget(cctx.Order.ID, "v1")
	stored, err := extRepo.ListByTarget(context.Background(), orderTarget)
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}
	if len(stored) != 1 || stored[0].Payload.StringValue == nil || *stored[0].Payload.StringValue != "d1" {
		t.Fatalf("order extensions = %+v", stored)
	}
}

// snapshotCtxProbe records the ctx CreateOrderStep passes to SnapshotCartItemToOrderItem.
type snapshotCtxProbe struct {
	inner interface {
		SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error
	}
	saw context.Context
}

func (p *snapshotCtxProbe) SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error {
	p.saw = ctx
	return p.inner.SnapshotCartItemToOrderItem(ctx, cartID, orderID, variantID, updatedBy)
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

	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, values, idleProducts(), idleComponents(), idlePrices())

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
