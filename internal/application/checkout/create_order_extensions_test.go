package checkout_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
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

func TestCreateOrderStep_PublishesOrderCreatedAfterExtensionSnapshot(t *testing.T) {
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

	bus := event.NewBus(logger.New("error"))
	var sawExtAtPublish bool
	bus.On(domainorder.EventOrderCreated, func(_ context.Context, evt event.Event) error {
		data := evt.Data.(domainorder.OrderCreatedData)
		orderTarget := domainext.OrderItemTarget(data.OrderID, "v1")
		stored, err := extRepo.ListByTarget(context.Background(), orderTarget)
		if err != nil {
			t.Fatalf("ListByTarget in handler: %v", err)
		}
		sawExtAtPublish = len(stored) == 1 && stored[0].Payload.StringValue != nil && *stored[0].Payload.StringValue == "d1"
		return nil
	})

	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, values, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))

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

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !sawExtAtPublish {
		t.Fatal("order.created fired before order_item extension snapshot was visible")
	}
}

type snapshotFailExt struct{}

func (snapshotFailExt) SnapshotCartItemToOrderItem(context.Context, string, string, string, string) error {
	return fmt.Errorf("snapshot boom")
}

type snapshotFailOnceExt struct {
	inner interface {
		SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error
	}
	failVariant string
	calls       map[string]int
}

func (p *snapshotFailOnceExt) SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error {
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	p.calls[variantID]++
	if variantID == p.failVariant && p.calls[variantID] == 1 {
		return fmt.Errorf("snapshot boom %s", variantID)
	}
	return p.inner.SnapshotCartItemToOrderItem(ctx, cartID, orderID, variantID, updatedBy)
}

func TestCreateOrderStep_SnapshotFailureStillPublishesOrderCreated(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	bus := event.NewBus(logger.New("error"))
	var published bool
	bus.On(domainorder.EventOrderCreated, func(context.Context, event.Event) error {
		published = true
		return nil
	})
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, snapshotFailExt{}, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))
	err := step.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "snapshot boom") {
		t.Fatalf("Execute err = %v, want snapshot boom", err)
	}
	if !published {
		t.Fatal("expected order.created even when snapshot fails")
	}
	if _, ok := cctx.GetMeta("created_order_id"); !ok {
		t.Fatal("expected created_order_id after save")
	}
	// Snapshot error fails the step (workflow will compensate → order.failed
	// after order.created; shopper retry creates a new order). Flag stays
	// unset so same-context re-entry can resume snapshots.
	if raw, ok := cctx.GetMeta("order_created_published"); ok && raw == true {
		t.Fatal("order_created_published must stay unset after snapshot failure")
	}
}

func TestCreateOrderStep_PublishFailureReturnsErrorAndResumePublishesOnce(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	bus := event.NewBus(logger.New("error"))
	var publishes int
	bus.On(domainorder.EventOrderCreated, func(context.Context, event.Event) error {
		publishes++
		if publishes == 1 {
			return fmt.Errorf("sync handler boom")
		}
		return nil
	})
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "publish order.created") {
		t.Fatalf("Execute err = %v, want publish error", err)
	}
	if publishes != 1 {
		t.Fatalf("publishes = %d, want 1", publishes)
	}
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("resume Execute: %v", err)
	}
	if publishes != 2 {
		t.Fatalf("publishes after resume = %d, want 2", publishes)
	}
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("idempotent Execute: %v", err)
	}
	if publishes != 2 {
		t.Fatalf("publishes after idempotent = %d, want 2", publishes)
	}
}

func TestCreateOrderStep_SnapshotPartialFailureResumesRemainingVariant(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1", "v2")}

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newMemSnapshotValueRepo()
	values := extensionapp.NewValueService(reg, extRepo)
	probe := &snapshotFailOnceExt{inner: values, failVariant: "v2"}

	bus := event.NewBus(logger.New("error"))
	var publishes int
	bus.On(domainorder.EventOrderCreated, func(context.Context, event.Event) error {
		publishes++
		return nil
	})

	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, probe, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")
	cctx.SetMeta("pricing", pricingContext037(t, "v1", "v2"))

	for _, vid := range []string{"v1", "v2"} {
		cartTarget := domainext.CartItemTarget(cctx.Cart.ID, vid)
		if _, err := values.UpsertBatch(context.Background(), cartTarget, []domainext.ValueInput{
			{FieldCode: "acme.gift.message", Value: "msg-" + vid},
		}, "cust-1", false); err != nil {
			t.Fatalf("UpsertBatch %s: %v", vid, err)
		}
	}

	err := step.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "snapshot boom v2") {
		t.Fatalf("Execute err = %v, want snapshot boom v2", err)
	}
	if publishes != 1 {
		t.Fatalf("publishes = %d, want 1 after partial snapshot", publishes)
	}
	orderID := cctx.Order.ID
	storedV1, err := extRepo.ListByTarget(context.Background(), domainext.OrderItemTarget(orderID, "v1"))
	if err != nil || len(storedV1) != 1 {
		t.Fatalf("v1 after first pass = %+v err=%v", storedV1, err)
	}
	storedV2, err := extRepo.ListByTarget(context.Background(), domainext.OrderItemTarget(orderID, "v2"))
	if err != nil {
		t.Fatalf("ListByTarget v2: %v", err)
	}
	if len(storedV2) != 0 {
		t.Fatalf("v2 should be missing after first pass, got %+v", storedV2)
	}

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("resume Execute: %v", err)
	}
	if publishes != 1 {
		t.Fatalf("publishes after resume = %d, want 1 (no republish)", publishes)
	}
	storedV2, err = extRepo.ListByTarget(context.Background(), domainext.OrderItemTarget(orderID, "v2"))
	if err != nil || len(storedV2) != 1 || storedV2[0].Payload.StringValue == nil || *storedV2[0].Payload.StringValue != "msg-v2" {
		t.Fatalf("v2 after resume = %+v err=%v", storedV2, err)
	}
}

func TestWorkflow_CreateOrderPublishFailureResumesOnce(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	bus := event.NewBus(logger.New("error"))
	var publishes int
	bus.On(domainorder.EventOrderCreated, func(context.Context, event.Event) error {
		publishes++
		if publishes == 1 {
			return fmt.Errorf("sync handler boom")
		}
		return nil
	})
	var failed bool
	bus.On(domainorder.EventOrderFailed, func(context.Context, event.Event) error {
		failed = true
		return nil
	})
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))
	next := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step, next}, bus, logger.New("error")).
		WithUnpaidOrderCompensation(orderRepo, nil, nil, nil)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := wf.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Workflow.Execute: %v", err)
	}
	if publishes != 2 {
		t.Fatalf("publishes = %d, want 2 (fail then resume)", publishes)
	}
	if failed {
		t.Fatal("order.failed must not fire when resume publish succeeds")
	}
	if cctx.Order == nil || cctx.Order.Status() != domainorder.OrderStatusPending {
		t.Fatalf("order status = %v, want pending after successful resume", cctx.Order)
	}
	raw, ok := cctx.GetMeta("order_created_published")
	if !ok || raw != true {
		t.Fatal("order_created_published must be set after workflow resume")
	}
}

func TestWorkflow_CreateOrderPartialSnapshotResumesRemainingVariant(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1", "v2")}

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newMemSnapshotValueRepo()
	values := extensionapp.NewValueService(reg, extRepo)
	probe := &snapshotFailOnceExt{inner: values, failVariant: "v2"}

	bus := event.NewBus(logger.New("error"))
	var publishes int
	bus.On(domainorder.EventOrderCreated, func(context.Context, event.Event) error {
		publishes++
		return nil
	})
	var failed bool
	bus.On(domainorder.EventOrderFailed, func(context.Context, event.Event) error {
		failed = true
		return nil
	})

	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, probe, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))
	next := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step, next}, bus, logger.New("error")).
		WithUnpaidOrderCompensation(orderRepo, nil, nil, nil)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")
	cctx.SetMeta("pricing", pricingContext037(t, "v1", "v2"))

	for _, vid := range []string{"v1", "v2"} {
		cartTarget := domainext.CartItemTarget(cctx.Cart.ID, vid)
		if _, err := values.UpsertBatch(context.Background(), cartTarget, []domainext.ValueInput{
			{FieldCode: "acme.gift.message", Value: "msg-" + vid},
		}, "cust-1", false); err != nil {
			t.Fatalf("UpsertBatch %s: %v", vid, err)
		}
	}

	if err := wf.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Workflow.Execute: %v", err)
	}
	if publishes != 1 {
		t.Fatalf("publishes = %d, want 1 (no republish on resume)", publishes)
	}
	if failed {
		t.Fatal("order.failed must not fire when resume snapshot succeeds")
	}
	orderID := cctx.Order.ID
	storedV2, err := extRepo.ListByTarget(context.Background(), domainext.OrderItemTarget(orderID, "v2"))
	if err != nil || len(storedV2) != 1 || storedV2[0].Payload.StringValue == nil || *storedV2[0].Payload.StringValue != "msg-v2" {
		t.Fatalf("v2 after workflow resume = %+v err=%v", storedV2, err)
	}
}

func TestWorkflow_CreateOrderResumeStillFailsCompensates(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	bus := event.NewBus(logger.New("error"))
	bus.On(domainorder.EventOrderCreated, func(context.Context, event.Event) error {
		return fmt.Errorf("sync handler always down")
	})
	var failed bool
	bus.On(domainorder.EventOrderFailed, func(context.Context, event.Event) error {
		failed = true
		return nil
	})
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, logger.New("error")).
		WithUnpaidOrderCompensation(orderRepo, nil, nil, nil)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := wf.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "publish order.created") {
		t.Fatalf("Workflow.Execute err = %v, want publish error", err)
	}
	if !failed {
		t.Fatal("expected order.failed after resume still fails")
	}
	if cctx.Order == nil || cctx.Order.Status() != domainorder.OrderStatusFailed {
		t.Fatalf("order status = %v, want failed", cctx.Order)
	}
}

func TestCreateOrderStep_ResumeFailsClosedWhenCartMissing(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	bus := event.NewBus(logger.New("error"))
	bus.On(domainorder.EventOrderCreated, func(context.Context, event.Event) error {
		return nil
	})
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, snapshotFailExt{}, idleProducts(), idleComponents(), idlePrices()).
		WithStockEventPublishing(bus, logger.New("error"))
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "snapshot boom") {
		t.Fatalf("first Execute err = %v, want snapshot boom", err)
	}
	cctx.Cart = nil
	err = step.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "cart missing") {
		t.Fatalf("resume err = %v, want cart missing", err)
	}
	if raw, ok := cctx.GetMeta("order_created_published"); ok && raw == true {
		t.Fatal("order_created_published must stay unset when cart is missing")
	}
}
