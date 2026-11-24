package pluginsdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/storefrontsession"
)

type cartPortMemExtRepo struct {
	values map[string]domainext.Value
}

func (m *cartPortMemExtRepo) key(v domainext.Value) string {
	return string(v.TargetType) + ":" + v.TargetID + ":" + v.FieldCode
}
func (m *cartPortMemExtRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	var out []domainext.Value
	for _, v := range m.values {
		if v.TargetType == target.Type && v.TargetID == target.ID {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *cartPortMemExtRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
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
func (m *cartPortMemExtRepo) Upsert(_ context.Context, value domainext.Value) error {
	if m.values == nil {
		m.values = map[string]domainext.Value{}
	}
	m.values[m.key(value)] = value
	return nil
}
func (m *cartPortMemExtRepo) UpsertBatch(_ context.Context, values []domainext.Value) error {
	for _, v := range values {
		if err := m.Upsert(context.Background(), v); err != nil {
			return err
		}
	}
	return nil
}

func (m *cartPortMemExtRepo) UpsertBatchSelective(_ context.Context, values []domainext.Value, exclusiveFieldCodes map[string]struct{}) error {
	for _, value := range values {
		if _, ok := exclusiveFieldCodes[value.FieldCode]; !ok || value.Payload.StringValue == nil {
			continue
		}
		key := m.key(value)
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
func (m *cartPortMemExtRepo) Delete(_ context.Context, target domainext.Target, code string) error {
	delete(m.values, string(target.Type)+":"+target.ID+":"+code)
	return nil
}
func (m *cartPortMemExtRepo) ListTargetIDsByTypeAndIDPrefix(context.Context, domainext.TargetType, string) ([]string, error) {
	return nil, nil
}
func (m *cartPortMemExtRepo) DeleteAllForTarget(context.Context, domainext.Target) error { return nil }

type cartPortMemCarts struct {
	carts  map[string]*domainCart.Cart
	getErr error
}

func (r *cartPortMemCarts) FindByID(_ context.Context, id string) (*domainCart.Cart, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	c, ok := r.carts[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	clone.Items = append([]domainCart.Item(nil), c.Items...)
	return &clone, nil
}
func (r *cartPortMemCarts) FindActiveByCustomerID(_ context.Context, customerID string) (*domainCart.Cart, error) {
	for _, c := range r.carts {
		if c.CustomerID == customerID && c.IsActive() {
			clone := *c
			clone.Items = append([]domainCart.Item(nil), c.Items...)
			return &clone, nil
		}
	}
	return nil, nil
}
func (r *cartPortMemCarts) Save(_ context.Context, c *domainCart.Cart) error {
	if r.carts == nil {
		r.carts = map[string]*domainCart.Cart{}
	}
	clone := *c
	clone.Items = append([]domainCart.Item(nil), c.Items...)
	r.carts[c.ID] = &clone
	return nil
}
func (r *cartPortMemCarts) Delete(_ context.Context, id string) error {
	delete(r.carts, id)
	return nil
}

func (r *cartPortMemCarts) DeleteEmptyGuestIfUnchanged(_ context.Context, cartID string, expectedVersion int) (bool, error) {
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

func (r *cartPortMemCarts) FindRecoveryCandidates(context.Context, time.Time, int) ([]*domainCart.Cart, error) {
	return nil, nil
}
func (r *cartPortMemCarts) MarkRecoveryEmailSent(context.Context, string, time.Time) (bool, error) {
	return false, nil
}

type cartPortPrices struct {
	currency string
}

func (p *cartPortPrices) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*domainpricing.Price, error) {
	cur := p.currency
	if cur == "" {
		cur = "EUR"
	}
	if currency != cur {
		return nil, nil
	}
	pr, _ := domainpricing.NewPrice("p-"+variantID, variantID, "", shared.MustNewMoney(1000, cur))
	return &pr, nil
}
func (p *cartPortPrices) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*domainpricing.Price, error) {
	out := map[string]*domainpricing.Price{}
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
func (p *cartPortPrices) ListByVariantID(context.Context, string) ([]domainpricing.Price, error) {
	return nil, nil
}
func (p *cartPortPrices) List(context.Context, int, int) ([]domainpricing.Price, error) {
	return nil, nil
}
func (p *cartPortPrices) Upsert(context.Context, *domainpricing.Price) error { return nil }

func bootCartPortFixture(t *testing.T) (*plugin.App, *cartApp.Service, *cartPortMemCarts, *extensionapp.Registry, *cartPortMemExtRepo) {
	t.Helper()
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Bus: event.NewBus(log)}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	hookReg := hooks.NewRegistry(nil)
	app.SetHookRegistry(hookReg)

	prices := &cartPortPrices{currency: "EUR"}
	pipeline := domainpricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), domainpricing.NewFinalizeStep())
	carts := &cartPortMemCarts{carts: map[string]*domainCart.Cart{}}
	repo := &cartPortMemExtRepo{values: map[string]domainext.Value{}}
	vr, pr := cartApp.PermissiveCatalogRepos()
	values := extensionapp.NewValueService(extReg, repo)
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, log, event.NewBus(log), values, hookReg)
	if err := app.BindCartPort(pluginsdk.NewCartPort(svc)); err != nil {
		t.Fatal(err)
	}
	return app, svc, carts, extReg, repo
}

func TestEnsureStorefrontCart_ReplacesNonActiveGuest(t *testing.T) {
	app, svc, carts, _, _ := bootCartPortFixture(t)
	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Checkout(); err != nil {
		t.Fatal(err)
	}
	if err := carts.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}

	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, ok := h.Cart()
	if !ok {
		t.Fatal("Cart()")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: storefrontsession.CartCookieName, Value: c.ID})
	view, err := port.EnsureStorefrontCart(req.Context(), rec, req, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.ID == c.ID {
		t.Fatalf("expected new active cart, got %+v", view)
	}
	gotCookie := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == storefrontsession.CartCookieName && cookie.Value == view.ID {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Fatal("expected Set-Cookie for new cart")
	}
}

type purgeFailValues struct {
	*extensionapp.ValueService
	err error
}

func (p *purgeFailValues) DeleteOrphanCartItemExtensions(context.Context, string, []string) error {
	return p.err
}

func TestDiscardEmptyGuestCart_ClearsCookieWhenPurgeFails(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Bus: event.NewBus(log)}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	prices := &cartPortPrices{currency: "EUR"}
	pipeline := domainpricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), domainpricing.NewFinalizeStep())
	carts := &cartPortMemCarts{carts: map[string]*domainCart.Cart{}}
	repo := &cartPortMemExtRepo{values: map[string]domainext.Value{}}
	vr, pr := cartApp.PermissiveCatalogRepos()
	inner := extensionapp.NewValueService(extReg, repo)
	values := &purgeFailValues{ValueService: inner, err: errors.New("purge denied")}
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, log, event.NewBus(log), values, nil)
	if err := app.BindCartPort(pluginsdk.NewCartPort(svc)); err != nil {
		t.Fatal(err)
	}
	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	storefrontsession.SetCartCookieSecure(rec, c.ID, false)

	err = port.DiscardEmptyGuestCart(req.Context(), rec, req, c.ID)
	if err == nil {
		t.Fatal("expected purge error")
	}
	if got, _ := carts.FindByID(context.Background(), c.ID); got != nil {
		t.Fatal("cart row must still be deleted when purge fails")
	}
	raw := rec.Header().Values("Set-Cookie")
	if len(raw) == 0 {
		t.Fatal("expected Set-Cookie headers")
	}
	last := raw[len(raw)-1]
	if !strings.Contains(last, storefrontsession.CartCookieName+"=") ||
		(!strings.Contains(last, "Max-Age=0") && !strings.Contains(last, "Max-Age=-1")) {
		t.Fatalf("purge failure must still clear cookie, last=%q all=%v", last, raw)
	}
}

func TestDiscardEmptyGuestCart_EnsureThenFailedAdd(t *testing.T) {
	app, svc, carts, _, _ := bootCartPortFixture(t)
	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()

	// First visit: no request cookie. Ensure creates a guest cart and queues Set-Cookie.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	view, err := port.EnsureStorefrontCart(req.Context(), rec, req, "EUR")
	if err != nil || view == nil {
		t.Fatalf("EnsureStorefrontCart: %v %+v", err, view)
	}
	setCookies := 0
	for _, c := range rec.Result().Cookies() {
		if c.Name == storefrontsession.CartCookieName && c.Value == view.ID && c.MaxAge > 0 {
			setCookies++
		}
	}
	if setCookies != 1 {
		t.Fatalf("want one new-cart Set-Cookie, got %d cookies=%v", setCookies, rec.Result().Cookies())
	}

	// Simulate failed first AddItem: discard the Ensure id (not CartIDFromRequest).
	if err := port.DiscardEmptyGuestCart(req.Context(), rec, req, view.ID); err != nil {
		t.Fatalf("DiscardEmptyGuestCart: %v", err)
	}
	if got, _ := carts.FindByID(context.Background(), view.ID); got != nil {
		t.Fatal("empty guest cart created by Ensure must be deleted")
	}
	// Result().Cookies() omits Max-Age=-1 expiry headers; inspect raw Set-Cookie.
	raw := rec.Header().Values("Set-Cookie")
	if len(raw) < 2 {
		t.Fatalf("want Ensure Set-Cookie then clear, got %v", raw)
	}
	last := raw[len(raw)-1]
	if !strings.Contains(last, storefrontsession.CartCookieName+"=") ||
		(!strings.Contains(last, "Max-Age=0") && !strings.Contains(last, "Max-Age=-1")) {
		t.Fatalf("last Set-Cookie must clear the cart, got %q (all=%v)", last, raw)
	}
	_ = svc
}

func TestEnsureStorefrontCart_PropagatesGetCartErrors(t *testing.T) {
	app, _, carts, _, _ := bootCartPortFixture(t)
	carts.getErr = errors.New("db timeout")
	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: storefrontsession.CartCookieName, Value: "cart-1"})
	_, err := port.EnsureStorefrontCart(req.Context(), rec, req, "EUR")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == storefrontsession.CartCookieName {
			t.Fatal("must not rewrite cookie on GetCart error")
		}
	}
}

func TestEnsureStorefrontCart_NotFoundCreatesCart(t *testing.T) {
	app, _, _, _, _ := bootCartPortFixture(t)
	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: storefrontsession.CartCookieName, Value: "missing-cart"})
	view, err := port.EnsureStorefrontCart(req.Context(), rec, req, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.ID == "" || view.ID == "missing-cart" {
		t.Fatalf("expected new cart, got %+v", view)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == storefrontsession.CartCookieName && c.Value == view.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Set-Cookie for replacement cart")
	}
}

func TestEnsureStorefrontCart_ForbiddenReplacesCookie(t *testing.T) {
	app, svc, carts, _, _ := bootCartPortFixture(t)
	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	_ = carts
	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: storefrontsession.CartCookieName, Value: c.ID})
	view, err := port.EnsureStorefrontCart(req.Context(), rec, req, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if view.ID == c.ID {
		t.Fatal("claimed customer cart id must be replaced for guest")
	}
}

func TestEnsureStorefrontCart_CookieSecureFromContext(t *testing.T) {
	app, _, _, _, _ := bootCartPortFixture(t)
	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Secure bit only on the ctx argument (not r.Context()) — must still apply.
	ctx := storefrontsession.WithCookieSecure(context.Background(), true)
	view, err := port.EnsureStorefrontCart(ctx, rec, req, "EUR")
	if err != nil || view == nil {
		t.Fatalf("EnsureStorefrontCart: %v %+v", err, view)
	}
	var got *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == storefrontsession.CartCookieName {
			got = c
		}
	}
	if got == nil || !got.Secure {
		t.Fatalf("expected Secure cookie from ctx WithCookieSecure, got %+v", got)
	}
}

func TestEnsureStorefrontCart_UsesStoreCurrency(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Bus: event.NewBus(log)}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	hookReg := hooks.NewRegistry(nil)
	app.SetHookRegistry(hookReg)
	prices := &cartPortPrices{currency: "PLN"}
	pipeline := domainpricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), domainpricing.NewFinalizeStep())
	carts := &cartPortMemCarts{carts: map[string]*domainCart.Cart{}}
	repo := &cartPortMemExtRepo{values: map[string]domainext.Value{}}
	vr, pr := cartApp.PermissiveCatalogRepos()
	values := extensionapp.NewValueService(extReg, repo)
	svc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, log, event.NewBus(log), values, hookReg)
	if err := app.BindCartPort(pluginsdk.NewCartPort(svc)); err != nil {
		t.Fatal(err)
	}

	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, ok := h.Cart()
	if !ok {
		t.Fatal("Cart()")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	st := &store.Store{ID: "s1", Currency: "PLN"}
	req = req.WithContext(store.WithStore(req.Context(), st))
	view, err := port.EnsureStorefrontCart(req.Context(), rec, req, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetCart(context.Background(), view.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Currency != "PLN" {
		t.Fatalf("currency = %q, want PLN", got.Currency)
	}
}

func TestCartPort_Get_HidesPrivateAndOtherOwners(t *testing.T) {
	app, svc, _, extReg, repo := bootCartPortFixture(t)
	if err := extReg.RegisterOwned(domainext.FieldDef{
		Code: "acme.note", Label: "Note", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Visibility: domainext.VisibilityPublic, Access: domainext.Access{PluginOnlyWrite: true},
	}, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := extReg.RegisterOwned(domainext.FieldDef{
		Code: "acme.design_id", Label: "Design", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Visibility: domainext.VisibilityPrivate, Access: domainext.Access{PluginOnlyWrite: true},
	}, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := extReg.RegisterOwned(domainext.FieldDef{
		Code: "other.secret", Label: "Secret", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Visibility: domainext.VisibilityPublic, Access: domainext.Access{PluginOnlyWrite: true},
	}, "other"); err != nil {
		t.Fatal(err)
	}
	if err := extReg.Register(domainext.FieldDef{
		Code: "core.private", Label: "Private", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Visibility: domainext.VisibilityPrivate,
	}); err != nil {
		t.Fatal(err)
	}

	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	ctx := extensionapp.WithPluginWrite(context.Background(), "acme", "acme.note", "acme.design_id")
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{
			{FieldCode: "acme.note", Value: "hello"},
			{FieldCode: "acme.design_id", Value: "d-42"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	ctxOther := extensionapp.WithPluginWrite(context.Background(), "other", "other.secret")
	if err := svc.UpsertLineExtension(ctxOther, c.ID, "cust-1", "var-1", []domainext.ValueInput{
		{FieldCode: "other.secret", Value: "hidden"},
	}, "other"); err != nil {
		t.Fatal(err)
	}
	priv := "nope"
	target := domainext.CartItemTarget(c.ID, "var-1")
	_ = repo.Upsert(context.Background(), domainext.Value{
		TargetType: target.Type, TargetID: target.ID, FieldCode: "core.private",
		Payload: domainext.ValuePayload{StringValue: &priv},
	})

	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()
	view, err := port.Get(context.Background(), c.ID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("items = %d", len(view.Items))
	}
	codes := map[string]interface{}{}
	for _, e := range view.Items[0].Extensions {
		codes[e.Code] = e.Value
	}
	if codes["acme.note"] != "hello" {
		t.Fatal("owner should see acme.note")
	}
	if codes["acme.design_id"] != "d-42" {
		t.Fatal("owner should see own private PluginOnlyWrite field")
	}
	if _, ok := codes["other.secret"]; ok {
		t.Fatal("must not see other plugin-only field")
	}
	if _, ok := codes["core.private"]; ok {
		t.Fatal("must not see private field")
	}

	// Unattributed Host strips private (including owned private plugin-only).
	unattr := pluginsdk.AsHost(app)
	unattr.Require(pluginsdk.CapCart)
	uport, ok := unattr.Cart()
	if !ok {
		t.Fatal("unattributed Cart() ok=false")
	}
	uview, err := uport.Get(context.Background(), c.ID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range uview.Items[0].Extensions {
		if e.Code == "acme.design_id" || e.Code == "core.private" {
			t.Fatalf("unattributed Host must not see private %q", e.Code)
		}
	}
}

func TestCartPort_DeleteLineExtension_OwnerScoped(t *testing.T) {
	app, svc, _, extReg, _ := bootCartPortFixture(t)
	if err := extReg.RegisterOwned(domainext.FieldDef{
		Code: "other.secret", Label: "Secret", Type: domainext.FieldTypeString,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Access: domainext.Access{PluginOnlyWrite: true},
	}, "other"); err != nil {
		t.Fatal(err)
	}
	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(context.Background(), c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatal(err)
	}
	ctxOther := extensionapp.WithPluginWrite(context.Background(), "other", "other.secret")
	if err := svc.UpsertLineExtension(ctxOther, c.ID, "cust-1", "var-1", []domainext.ValueInput{
		{FieldCode: "other.secret", Value: "x"},
	}, "other"); err != nil {
		t.Fatal(err)
	}

	h := pluginsdk.AsHostFor(app, "acme")
	h.Require(pluginsdk.CapCart)
	port, _ := h.Cart()
	err = port.DeleteLineExtension(context.Background(), c.ID, "cust-1", "var-1", "other.secret")
	if err == nil || !apperror.Is(err, apperror.CodeForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestCartView_JSONRoundTrip(t *testing.T) {
	_, svc, _, extReg, _ := bootCartPortFixture(t)
	if err := extReg.Register(domainext.FieldDef{
		Code: "meta.json", Label: "JSON", Type: domainext.FieldTypeJSON,
		Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored,
		Visibility: domainext.VisibilityPublic,
	}); err != nil {
		t.Fatal(err)
	}
	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddItem(context.Background(), c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{
		Extensions: []domainext.ValueInput{{FieldCode: "meta.json", Value: map[string]interface{}{"a": float64(1)}}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetCart(context.Background(), c.ID, "cust-1")
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.CartView(context.Background(), got)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items[0].Extensions) != 1 {
		t.Fatalf("exts = %+v", view.Items[0].Extensions)
	}
	raw, err := json.Marshal(view.Items[0].Extensions[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("value not structured JSON: %v (%T)", err, view.Items[0].Extensions[0].Value)
	}
	if decoded["a"] != float64(1) {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestAsHostFor_RegisterField_RecordsOwnerOutsideInitAll(t *testing.T) {
	app := &plugin.App{}
	extReg := extensionapp.NewRegistry()
	app.SetExtensionRegistry(extReg)
	h := pluginsdk.AsHostFor(app, "acme/demo")
	if err := h.RegisterField(extapi.FieldDef{
		Code: "acme.f", Label: "F", Type: extapi.FieldTypeString,
		Scope: extapi.TargetCartItem, StorageMode: extapi.StorageStored,
		Access: extapi.Access{PluginOnlyWrite: true},
	}); err != nil {
		t.Fatal(err)
	}
	if got := extReg.Owner("acme.f"); got != "acme/demo" {
		t.Fatalf("owner = %q, want acme/demo", got)
	}
}

func TestCartIDFromRequest_TrimsWhitespace(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: storefrontsession.CartCookieName, Value: "   "})
	if _, ok := storefrontsession.CartIDFromRequest(req); ok {
		t.Fatal("whitespace cookie must be absent")
	}
}
