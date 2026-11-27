package extref_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/identity"
	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/extref"
)

func TestAddItemWithGiftNote_ThroughCartService(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
	}{
		{"background", context.Background()},
		{"guest", auth.WithIdentity(context.Background(), identity.Guest())},
		{"customer", auth.WithIdentity(context.Background(), mustID(t, "cust-1", identity.RoleCustomer))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, cartID := bootExtrefCartHost(t)
			if err := extref.AddItemWithGiftNote(tc.ctx, h, cartID, "cust-1", "var-1", 1, "wrap please"); err != nil {
				t.Fatalf("AddItemWithGiftNote: %v", err)
			}
		})
	}
}

func TestGiftNote_CanRewrite_NotExclusive(t *testing.T) {
	h, cartID := bootExtrefCartHost(t)
	port, ok := h.Cart()
	if !ok {
		t.Fatal("Cart()")
	}
	ctx := h.WithPluginWrite(context.Background(), extref.PluginName, extref.FieldCodeGiftNote)
	if err := port.AddItem(ctx, cartID, "cust-1", "var-1", 1, pluginsdk.CartAddItemOpts{
		Extensions: []pluginsdk.CartExtensionValue{{Code: extref.FieldCodeGiftNote, Value: "first"}},
		UpdatedBy:  "plugin:" + extref.PluginName,
	}); err != nil {
		t.Fatal(err)
	}
	if err := port.UpsertLineExtension(ctx, cartID, "cust-1", "var-1", []pluginsdk.CartExtensionValue{
		{Code: extref.FieldCodeGiftNote, Value: "second"},
	}, "plugin:"+extref.PluginName); err != nil {
		t.Fatalf("gift_note rewrite: %v", err)
	}
	view, err := port.Get(context.Background(), cartID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, e := range view.Items[0].Extensions {
		if e.Code == extref.FieldCodeGiftNote {
			got, _ = e.Value.(string)
		}
	}
	if got != "second" {
		t.Fatalf("gift_note = %q, want second", got)
	}
}

func mustID(t *testing.T, userID string, role identity.Role) identity.Identity {
	t.Helper()
	id, err := identity.NewIdentity(userID, role)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type memExtRepo struct {
	values map[string]domainext.Value
}

func (m *memExtRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	var out []domainext.Value
	for _, v := range m.values {
		if v.TargetType == target.Type && v.TargetID == target.ID {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *memExtRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
	want := map[string]struct{}{}
	for _, id := range targetIDs {
		want[id] = struct{}{}
	}
	var out []domainext.Value
	for _, v := range m.values {
		if v.TargetType != targetType {
			continue
		}
		if _, ok := want[v.TargetID]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *memExtRepo) Upsert(_ context.Context, value domainext.Value) error {
	if m.values == nil {
		m.values = map[string]domainext.Value{}
	}
	m.values[string(value.TargetType)+":"+value.TargetID+":"+value.FieldCode] = value
	return nil
}
func (m *memExtRepo) UpsertBatch(_ context.Context, values []domainext.Value) error {
	for _, v := range values {
		if err := m.Upsert(context.Background(), v); err != nil {
			return err
		}
	}
	return nil
}

func (m *memExtRepo) UpsertBatchSelective(_ context.Context, values []domainext.Value, exclusiveFieldCodes map[string]struct{}) error {
	for _, value := range values {
		if _, ok := exclusiveFieldCodes[value.FieldCode]; !ok || value.Payload.StringValue == nil {
			continue
		}
		key := string(value.TargetType) + ":" + value.TargetID + ":" + value.FieldCode
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
func (m *memExtRepo) Delete(context.Context, domainext.Target, string) error { return nil }
func (m *memExtRepo) ListTargetIDsByTypeAndIDPrefix(context.Context, domainext.TargetType, string) ([]string, error) {
	return nil, nil
}
func (m *memExtRepo) DeleteAllForTarget(context.Context, domainext.Target) error { return nil }

type memCartRepo struct {
	carts map[string]*domainCart.Cart
}

func (r *memCartRepo) FindByID(_ context.Context, id string) (*domainCart.Cart, error) {
	c, ok := r.carts[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	clone.Items = append([]domainCart.Item(nil), c.Items...)
	return &clone, nil
}
func (r *memCartRepo) FindActiveByCustomerID(_ context.Context, customerID string) (*domainCart.Cart, error) {
	for _, c := range r.carts {
		if c.CustomerID == customerID && c.IsActive() {
			clone := *c
			clone.Items = append([]domainCart.Item(nil), c.Items...)
			return &clone, nil
		}
	}
	return nil, nil
}
func (r *memCartRepo) Save(_ context.Context, c *domainCart.Cart) error {
	if r.carts == nil {
		r.carts = map[string]*domainCart.Cart{}
	}
	clone := *c
	clone.Items = append([]domainCart.Item(nil), c.Items...)
	r.carts[c.ID] = &clone
	return nil
}
func (r *memCartRepo) Delete(_ context.Context, id string) error {
	delete(r.carts, id)
	return nil
}

func (r *memCartRepo) DeleteEmptyGuestIfUnchanged(_ context.Context, cartID string, expectedVersion int) (bool, error) {
	c, err := r.FindByID(context.Background(), cartID)
	if err != nil {
		return false, err
	}
	if c == nil || c.CustomerID != "" || c.ItemCount() > 0 || c.Version != expectedVersion {
		return false, nil
	}
	if err := r.Delete(context.Background(), cartID); err != nil {
		return false, err
	}
	return true, nil
}

func (r *memCartRepo) FindRecoveryCandidates(context.Context, time.Time, int) ([]*domainCart.Cart, error) {
	return nil, nil
}
func (r *memCartRepo) MarkRecoveryEmailSent(context.Context, string, time.Time) (bool, error) {
	return false, nil
}
func (r *memCartRepo) ClearRecoveryEmailSent(context.Context, string) error { return nil }

type priceStub struct {
	prices map[string]*domainpricing.Price
}

func newPriceStub() *priceStub {
	p := &priceStub{prices: map[string]*domainpricing.Price{}}
	pr, _ := domainpricing.NewPrice("p-var-1", "var-1", "", shared.MustNewMoney(1000, "EUR"))
	p.prices["var-1:EUR:"] = &pr
	return p
}

func (p *priceStub) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*domainpricing.Price, error) {
	return p.prices[variantID+":"+currency+":"+storeID], nil
}
func (p *priceStub) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*domainpricing.Price, error) {
	out := make(map[string]*domainpricing.Price, len(variantIDs))
	for _, id := range variantIDs {
		pr, err := p.FindByVariantCurrencyAndStore(ctx, id, currency, storeID)
		if err != nil {
			return nil, err
		}
		if pr != nil {
			out[id] = pr
		}
	}
	return out, nil
}
func (p *priceStub) ListByVariantID(context.Context, string) ([]domainpricing.Price, error) {
	return nil, nil
}
func (p *priceStub) List(context.Context, int, int) ([]domainpricing.Price, error) { return nil, nil }
func (p *priceStub) Upsert(context.Context, *domainpricing.Price) error            { return nil }

func bootExtrefCartHost(t *testing.T) (pluginsdk.Host, string) {
	t.Helper()
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Bus: event.NewBus(log)}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	hookReg := hooks.NewRegistry(nil)
	app.SetHookRegistry(hookReg)

	bridged := pluginsdk.Adapt(extref.New())
	if err := bridged.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	prices := newPriceStub()
	pipeline := domainpricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), domainpricing.NewFinalizeStep())
	carts := &memCartRepo{carts: map[string]*domainCart.Cart{}}
	vr, pr := cartApp.PermissiveCatalogRepos()
	values := extensionapp.NewValueService(extReg, &memExtRepo{values: map[string]domainext.Value{}})
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, log, event.NewBus(log), values, hookReg)
	if err := app.BindCartPort(pluginsdk.NewCartPort(svc)); err != nil {
		t.Fatal(err)
	}
	if err := app.AssertNeedsMet(); err != nil {
		t.Fatal(err)
	}
	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	return pluginsdk.AsHostFor(app, extref.PluginName), c.ID
}
