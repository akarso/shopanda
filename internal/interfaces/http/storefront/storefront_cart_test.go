package storefront_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/testutil"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/composition"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
)

type failExistingSaveRepo struct {
	*storefrontCartRepoStub
}

func (r *failExistingSaveRepo) Save(ctx context.Context, c *domainCart.Cart) error {
	if _, exists := r.carts[c.ID]; exists {
		return fmt.Errorf("disk full")
	}
	return r.storefrontCartRepoStub.Save(ctx, c)
}

func (r *storefrontCartRepoStub) count() int {
	return len(r.carts)
}

type storefrontCartRepoStub struct {
	carts map[string]*domainCart.Cart
}

func newStorefrontCartRepoStub() *storefrontCartRepoStub {
	return &storefrontCartRepoStub{carts: make(map[string]*domainCart.Cart)}
}

func (r *storefrontCartRepoStub) FindByID(_ context.Context, id string) (*domainCart.Cart, error) {
	c, ok := r.carts[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	clone.Items = append([]domainCart.Item(nil), c.Items...)
	return &clone, nil
}

func (r *storefrontCartRepoStub) FindActiveByCustomerID(_ context.Context, customerID string) (*domainCart.Cart, error) {
	for _, c := range r.carts {
		if c.CustomerID == customerID && c.IsActive() {
			clone := *c
			clone.Items = append([]domainCart.Item(nil), c.Items...)
			return &clone, nil
		}
	}
	return nil, nil
}

func (r *storefrontCartRepoStub) Save(_ context.Context, c *domainCart.Cart) error {
	clone := *c
	clone.Items = append([]domainCart.Item(nil), c.Items...)
	r.carts[c.ID] = &clone
	return nil
}

func (r *storefrontCartRepoStub) Delete(_ context.Context, id string) error {
	delete(r.carts, id)
	return nil
}

func (r *storefrontCartRepoStub) FindRecoveryCandidates(_ context.Context, _ time.Time, _ int) ([]*domainCart.Cart, error) {
	return nil, nil
}

func (r *storefrontCartRepoStub) MarkRecoveryEmailSent(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}

type storefrontPriceRepoStub struct {
	prices map[string]*pricing.Price
}

func newStorefrontPriceRepoStub() *storefrontPriceRepoStub {
	return &storefrontPriceRepoStub{prices: make(map[string]*pricing.Price)}
}

func (r *storefrontPriceRepoStub) set(variantID, currency string, amount int64) {
	key := variantID + ":" + currency + ":"
	p, _ := pricing.NewPrice("price-"+key, variantID, "", shared.MustNewMoney(amount, currency))
	r.prices[key] = &p
}

func (r *storefrontPriceRepoStub) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	return r.prices[variantID+":"+currency+":"+storeID], nil
}

func (r *storefrontPriceRepoStub) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, r.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

func (r *storefrontPriceRepoStub) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (r *storefrontPriceRepoStub) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

func (r *storefrontPriceRepoStub) Upsert(_ context.Context, _ *pricing.Price) error { return nil }

type mockStorefrontVariantRepo struct {
	findByIDFn        func(ctx context.Context, id string) (*catalog.Variant, error)
	listByProductIDFn func(ctx context.Context, productID string, offset, limit int) ([]catalog.Variant, error)
}

func (m *mockStorefrontVariantRepo) FindByID(ctx context.Context, id string) (*catalog.Variant, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockStorefrontVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockStorefrontVariantRepo) FindBySKUs(_ context.Context, _ []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}

func (m *mockStorefrontVariantRepo) ListByProductID(ctx context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	if m.listByProductIDFn != nil {
		return m.listByProductIDFn(ctx, productID, offset, limit)
	}
	return nil, nil
}

func (m *mockStorefrontVariantRepo) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, m.ListByProductID, productIDs, limitPerProduct)
}

func (m *mockStorefrontVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockStorefrontVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

func newStorefrontCartService() (*cartApp.Service, *storefrontCartRepoStub, *storefrontPriceRepoStub) {
	return newStorefrontCartServiceWith(nil, nil)
}

func newStorefrontCartServiceWith(values *extensionapp.ValueService, hookReg *hooks.Registry) (*cartApp.Service, *storefrontCartRepoStub, *storefrontPriceRepoStub) {
	carts := newStorefrontCartRepoStub()
	prices := newStorefrontPriceRepoStub()
	log := logger.NewWithWriter(io.Discard, "error")
	pipeline := pricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), pricing.NewFinalizeStep())
	vr, pr := cartApp.PermissiveCatalogRepos()
	// Pass untyped nil so the cart service's extension writer interface stays
	// nil (a typed *ValueService(nil) would panic on claim cleanup).
	if values != nil {
		return cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, log, event.NewBus(log), values, hookReg), carts, prices
	}
	return cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, log, event.NewBus(log), nil, hookReg), carts, prices
}

func TestStorefrontHandler_Product_RendersCartForm(t *testing.T) {
	repo := &mockStorefrontRepo{findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
		return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug, Description: "A fine widget"}, nil
	}}
	variants := &mockStorefrontVariantRepo{listByProductIDFn: func(_ context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
		return []catalog.Variant{{ID: "var-1", ProductID: productID, SKU: "WID-1"}}, nil
	}}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, _ := newStorefrontCartService()
	h := storefront.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "action=\"/cart/add\"") {
		t.Fatalf("body missing cart form action: %s", body)
	}
	if !strings.Contains(body, "name=\"variant_id\" value=\"var-1\"") {
		t.Fatalf("body missing variant id: %s", body)
	}
	if !strings.Contains(body, "Add to cart") {
		t.Fatalf("body missing add to cart button: %s", body)
	}
}

func TestStorefrontHandler_CartAdd_SetsCookie_AndCountFragment(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	form := url.Values{"variant_id": {"var-1"}, "quantity": {"2"}, "redirect_to": {"/products/widget"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cart/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	var cartCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_cart" {
			cartCookie = cookie
			break
		}
	}
	if cartCookie == nil {
		t.Fatal("expected storefront cart cookie to be set")
	}
	if cartCookie.Secure {
		t.Fatal("guest cart cookie should not be secure on plain HTTP requests")
	}
	countRec := httptest.NewRecorder()
	countReq := httptest.NewRequest("GET", "/fragments/cart-count", nil)
	countReq.AddCookie(cartCookie)
	newStorefrontRouter(h).ServeHTTP(countRec, countReq)

	if got := strings.TrimSpace(countRec.Body.String()); got != "Cart (2)" {
		t.Fatalf("cart count = %q, want %q", got, "Cart (2)")
	}
}

func TestStorefrontHandler_CartAdd_InvalidRedirect_FallsBackToCart(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	form := url.Values{"variant_id": {"var-1"}, "quantity": {"1"}, "redirect_to": {"https://evil.example/phish"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cart/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/cart" {
		t.Fatalf("Location = %q, want %q", location, "/cart")
	}
}

func TestStorefrontHandler_CartAdd_LogsInternalError(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	var logs bytes.Buffer
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithLog(logger.NewWithWriter(&logs, "error"))

	form := url.Values{"variant_id": {"var-1"}, "quantity": {"1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cart/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Request-ID", "req-123")
	handler := httpshared.RequestIDMiddleware()(newStorefrontRouter(h))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	output := logs.String()
	if !strings.Contains(output, `"event":"storefront.cart.add.ensure_failed"`) {
		t.Fatalf("expected structured cart error event, got %s", output)
	}
	if !strings.Contains(output, `"request_id":"req-123"`) {
		t.Fatalf("expected request_id in structured cart error log, got %s", output)
	}
	if !strings.Contains(output, `"variant_id":"var-1"`) {
		t.Fatalf("expected variant_id in structured cart error log, got %s", output)
	}
	if !strings.Contains(output, `"message":"storefront cart service is not configured"`) {
		t.Fatalf("expected underlying error in structured cart error log, got %s", output)
	}
}

func TestStorefrontHandler_CartAdd_SetsSecureCookie_OnHTTPS(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	form := url.Values{"variant_id": {"var-1"}, "quantity": {"1"}, "redirect_to": {"/products/widget"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "https://example.test/cart/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.TLS = &tls.ConnectionState{}
	newStorefrontRouter(h).ServeHTTP(rec, req)

	var cartCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_cart" {
			cartCookie = cookie
			break
		}
	}
	if cartCookie == nil {
		t.Fatal("expected storefront cart cookie to be set")
	}
	if !cartCookie.Secure {
		t.Fatal("guest cart cookie should be secure on HTTPS requests")
	}
}

func TestStorefrontHandler_Cart_OK(t *testing.T) {
	repo := &mockStorefrontRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
		return &catalog.Product{ID: id, Name: "Trail Shoe", Slug: "trail-shoe"}, nil
	}}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "p1", SKU: "SHOE-42", Name: "Size 42"}, nil
	}}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 2, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	h := storefront.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/cart", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Shopping Cart") {
		t.Fatalf("body missing cart title: %s", body)
	}
	if !strings.Contains(body, "Trail Shoe") {
		t.Fatalf("body missing product name: %s", body)
	}
	if !strings.Contains(body, "value=\"2\"") {
		t.Fatalf("body missing quantity: %s", body)
	}
	if !strings.Contains(body, "EUR 30.00") {
		t.Fatalf("body missing subtotal: %s", body)
	}
}

func TestStorefrontHandler_CartUpdate_HTMX(t *testing.T) {
	repo := &mockStorefrontRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
		return &catalog.Product{ID: id, Name: "Trail Shoe", Slug: "trail-shoe"}, nil
	}}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "p1", SKU: "SHOE-42", Name: "Size 42"}, nil
	}}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	currentCart, _ := cartSvc.CreateCart(context.Background(), "", "EUR")
	_, _ = cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	h := storefront.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

	form := url.Values{"variant_id": {"var-1"}, "quantity": {"3"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cart/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Header().Get("HX-Trigger") != "cart-updated" {
		t.Fatalf("HX-Trigger = %q, want %q", rec.Header().Get("HX-Trigger"), "cart-updated")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "value=\"3\"") {
		t.Fatalf("body missing updated quantity: %s", body)
	}
	if !strings.Contains(body, "EUR 45.00") {
		t.Fatalf("body missing updated subtotal: %s", body)
	}
}

func TestStorefrontHandler_CartRemove_HTMX(t *testing.T) {
	repo := &mockStorefrontRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
		return &catalog.Product{ID: id, Name: "Trail Shoe", Slug: "trail-shoe"}, nil
	}}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "p1", SKU: "SHOE-42", Name: "Size 42"}, nil
	}}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	currentCart, _ := cartSvc.CreateCart(context.Background(), "", "EUR")
	_, _ = cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{})
	h := storefront.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

	form := url.Values{"variant_id": {"var-1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cart/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Your cart is empty.") {
		t.Fatalf("body missing empty message: %s", body)
	}
}

func storefrontCSRFToken(t *testing.T, h *storefront.StorefrontHandler) (token string, cookies []*http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/storefront/csrf", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("csrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("csrf decode: %v", err)
	}
	if env.Data.CSRFToken == "" {
		t.Fatal("empty csrf_token")
	}
	return env.Data.CSRFToken, rec.Result().Cookies()
}

func TestStorefrontHandler_JSONCartAdd_SetsCookieAndCounts(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	token, cookies := storefrontCSRFToken(t, h)
	body := `{"variant_id":"var-1","quantity":2}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storefront/cart/items", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpshared.CSRFHeaderName, token)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	newStorefrontRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"total_quantity":2`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "cart_id") {
		t.Fatal("JSON add must not expose cart_id")
	}
	var cartCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "shopanda_storefront_cart" {
			cartCookie = c
			break
		}
	}
	if cartCookie == nil {
		t.Fatal("expected storefront cart cookie")
	}
	countRec := httptest.NewRecorder()
	countReq := httptest.NewRequest(http.MethodGet, "/fragments/cart-count", nil)
	countReq.AddCookie(cartCookie)
	newStorefrontRouter(h).ServeHTTP(countRec, countReq)
	if got := strings.TrimSpace(countRec.Body.String()); got != "Cart (2)" {
		t.Fatalf("cart count = %q", got)
	}
}

func TestStorefrontHandler_JSONCartAdd_RequiresCSRF(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storefront/cart/items", strings.NewReader(`{"variant_id":"var-1"}`))
	req.Header.Set("Content-Type", "application/json")
	newStorefrontRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if storefrontJSONErrorCode(t, rec) != "forbidden" {
		t.Fatalf("csrf error code = %s body=%s", storefrontJSONErrorCode(t, rec), rec.Body.String())
	}
}

func TestStorefrontHandler_JSONCartAdd_RejectsMismatchedCSRF(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	rec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":1}`, storefrontJSONCartCall{CSRFToken: "deadbeef"})
	if rec.Code != http.StatusForbidden || storefrontJSONErrorCode(t, rec) != "forbidden" {
		t.Fatalf("status=%d code=%s body=%s", rec.Code, storefrontJSONErrorCode(t, rec), rec.Body.String())
	}
}

func TestStorefrontHandler_JSONCartAdd_RejectsFormBody(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	token, cookies := storefrontCSRFToken(t, h)
	form := url.Values{"variant_id": {"var-1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storefront/cart/items", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(httpshared.CSRFHeaderName, token)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	newStorefrontRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 body=%s", rec.Code, rec.Body.String())
	}
}

type storefrontJSONCartCall struct {
	CustomerID  string
	Cookies     []*http.Cookie
	ContentType string
	CSRFToken   string
	SkipCSRF    bool
}

func storefrontJSONCartRequest(t *testing.T, h *storefront.StorefrontHandler, method, path, body string, call storefrontJSONCartCall) *httptest.ResponseRecorder {
	t.Helper()
	token, cookies := storefrontCSRFToken(t, h)
	if call.CSRFToken != "" {
		token = call.CSRFToken
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" || call.ContentType != "" {
		ct := call.ContentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}
	if !call.SkipCSRF {
		req.Header.Set(httpshared.CSRFHeaderName, token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	for _, c := range call.Cookies {
		req.AddCookie(c)
	}
	if call.CustomerID != "" {
		req = testhelper.CustomerRequest(req, call.CustomerID)
	}
	newStorefrontRouter(h).ServeHTTP(rec, req)
	return rec
}

func parseStorefrontJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var env map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return env
}

func storefrontJSONErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	env := parseStorefrontJSON(t, rec)
	errBody, _ := env["error"].(map[string]interface{})
	if errBody == nil {
		t.Fatalf("missing error envelope: %s", rec.Body.String())
	}
	code, _ := errBody["code"].(string)
	return code
}

func storefrontJSONErrorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	env := parseStorefrontJSON(t, rec)
	errBody, _ := env["error"].(map[string]interface{})
	if errBody == nil {
		t.Fatalf("missing error envelope: %s", rec.Body.String())
	}
	msg, _ := errBody["message"].(string)
	return msg
}

func storefrontCartCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "shopanda_storefront_cart" {
			return c
		}
	}
	t.Fatal("expected storefront cart cookie")
	return nil
}

func TestStorefrontHandler_JSONCartUpdateAndRemove(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	addRec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":2}`, storefrontJSONCartCall{})
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status = %d body=%s", addRec.Code, addRec.Body.String())
	}
	cartCookie := storefrontCartCookie(t, addRec)

	updRec := storefrontJSONCartRequest(t, h, http.MethodPut, "/storefront/cart/items/var-1", `{"quantity":3}`, storefrontJSONCartCall{Cookies: []*http.Cookie{cartCookie}})
	if updRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updRec.Code, updRec.Body.String())
	}
	if !strings.Contains(updRec.Body.String(), `"total_quantity":3`) {
		t.Fatalf("update body = %s", updRec.Body.String())
	}

	delRec := storefrontJSONCartRequest(t, h, http.MethodDelete, "/storefront/cart/items/var-1", "", storefrontJSONCartCall{Cookies: []*http.Cookie{cartCookie}})
	if delRec.Code != http.StatusOK {
		t.Fatalf("remove status = %d body=%s", delRec.Code, delRec.Body.String())
	}
	if !strings.Contains(delRec.Body.String(), `"total_quantity":0`) {
		t.Fatalf("remove body = %s", delRec.Body.String())
	}
	if strings.Contains(delRec.Body.String(), "cart_id") {
		t.Fatal("JSON remove must not expose cart_id")
	}

	countRec := httptest.NewRecorder()
	countReq := httptest.NewRequest(http.MethodGet, "/fragments/cart-count", nil)
	countReq.AddCookie(cartCookie)
	newStorefrontRouter(h).ServeHTTP(countRec, countReq)
	if got := strings.TrimSpace(countRec.Body.String()); got != "Cart (0)" {
		t.Fatalf("cart count = %q", got)
	}
}

func TestStorefrontHandler_JSONCartUpdate_RequiresExistingCart(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	rec := storefrontJSONCartRequest(t, h, http.MethodPut, "/storefront/cart/items/var-1", `{"quantity":2}`, storefrontJSONCartCall{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", rec.Code, rec.Body.String())
	}
}

func TestStorefrontHandler_JSONCartUpdate_RequiresCSRF(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, _ := newStorefrontCartService()
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/storefront/cart/items/var-1", strings.NewReader(`{"quantity":2}`))
	req.Header.Set("Content-Type", "application/json")
	newStorefrontRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if storefrontJSONErrorCode(t, rec) != "forbidden" {
		t.Fatalf("csrf error code = %s body=%s", storefrontJSONErrorCode(t, rec), rec.Body.String())
	}
}

func TestStorefrontHandler_JSONCSRF_NoStoreAndCookie(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, _ := newStorefrontCartService()
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	rec := httptest.NewRecorder()
	newStorefrontRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/storefront/csrf", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "shopanda_csrf" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected shopanda_csrf Set-Cookie")
	}
	env := parseStorefrontJSON(t, rec)
	data := env["data"].(map[string]interface{})
	if data["csrf_token"] == "" {
		t.Fatal("missing data.csrf_token")
	}
}

func TestStorefrontHandler_JSONCartAdd_QuantityRules(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	omitted := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1"}`, storefrontJSONCartCall{})
	if omitted.Code != http.StatusOK {
		t.Fatalf("omitted quantity status = %d body=%s", omitted.Code, omitted.Body.String())
	}
	if !strings.Contains(omitted.Body.String(), `"total_quantity":1`) {
		t.Fatalf("omitted quantity body = %s", omitted.Body.String())
	}

	zero := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":0}`, storefrontJSONCartCall{})
	if zero.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, zero) != "validation" {
		t.Fatalf("quantity 0: status=%d code=%s body=%s", zero.Code, storefrontJSONErrorCode(t, zero), zero.Body.String())
	}
	if _, ok := findCookie(zero, "shopanda_storefront_cart"); ok {
		t.Fatal("quantity 0 must not mint a cart cookie")
	}

	neg := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":-2}`, storefrontJSONCartCall{})
	if neg.Code != http.StatusUnprocessableEntity {
		t.Fatalf("negative quantity status = %d", neg.Code)
	}

	nullQty := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":null}`, storefrontJSONCartCall{})
	if nullQty.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, nullQty) != "validation" {
		t.Fatalf("quantity null: status=%d code=%s body=%s", nullQty.Code, storefrontJSONErrorCode(t, nullQty), nullQty.Body.String())
	}
	if _, ok := findCookie(nullQty, "shopanda_storefront_cart"); ok {
		t.Fatal("quantity null must not mint a cart cookie")
	}
}

func TestStorefrontHandler_JSONCartAdd_RejectsTrailingJSON(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	rec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1"}{"x":1}`, storefrontJSONCartCall{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := findCookie(rec, "shopanda_storefront_cart"); ok {
		t.Fatal("trailing json must not mint a cart cookie")
	}
}

func TestStorefrontHandler_JSONCartAdd_RejectsJSONP(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	rec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1"}`, storefrontJSONCartCall{ContentType: "application/jsonp"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 body=%s", rec.Code, rec.Body.String())
	}
}

func TestStorefrontHandler_JSONCartUpdate_UnknownVariantAndZeroQty(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	addRec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":1}`, storefrontJSONCartCall{})
	cartCookie := storefrontCartCookie(t, addRec)

	zero := storefrontJSONCartRequest(t, h, http.MethodPut, "/storefront/cart/items/var-1", `{"quantity":0}`, storefrontJSONCartCall{Cookies: []*http.Cookie{cartCookie}})
	if zero.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, zero) != "validation" {
		t.Fatalf("qty 0 status=%d code=%s body=%s", zero.Code, storefrontJSONErrorCode(t, zero), zero.Body.String())
	}

	nullQty := storefrontJSONCartRequest(t, h, http.MethodPut, "/storefront/cart/items/var-1", `{"quantity":null}`, storefrontJSONCartCall{Cookies: []*http.Cookie{cartCookie}})
	if nullQty.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, nullQty) != "validation" {
		t.Fatalf("qty null status=%d code=%s body=%s", nullQty.Code, storefrontJSONErrorCode(t, nullQty), nullQty.Body.String())
	}
	if storefrontJSONErrorMessage(t, nullQty) != "quantity must be positive" {
		t.Fatalf("qty null message = %q", storefrontJSONErrorMessage(t, nullQty))
	}

	missing := storefrontJSONCartRequest(t, h, http.MethodPut, "/storefront/cart/items/no-such", `{"quantity":2}`, storefrontJSONCartCall{Cookies: []*http.Cookie{cartCookie}})
	if missing.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, missing) != "validation" {
		t.Fatalf("unknown variant status=%d code=%s body=%s", missing.Code, storefrontJSONErrorCode(t, missing), missing.Body.String())
	}
}

func TestStorefrontHandler_JSONCartRemove_RequiresCartAndCSRF(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, _, _ := newStorefrontCartService()
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	noCart := storefrontJSONCartRequest(t, h, http.MethodDelete, "/storefront/cart/items/var-1", "", storefrontJSONCartCall{})
	if noCart.Code != http.StatusNotFound {
		t.Fatalf("no cart status = %d body=%s", noCart.Code, noCart.Body.String())
	}

	noCSRF := storefrontJSONCartRequest(t, h, http.MethodDelete, "/storefront/cart/items/var-1", "", storefrontJSONCartCall{SkipCSRF: true})
	if noCSRF.Code != http.StatusForbidden || storefrontJSONErrorCode(t, noCSRF) != "forbidden" {
		t.Fatalf("no csrf status=%d code=%s body=%s", noCSRF.Code, storefrontJSONErrorCode(t, noCSRF), noCSRF.Body.String())
	}
}

func TestStorefrontHandler_JSONCartAdd_ExtensionErrors(t *testing.T) {
	reg := extensionapp.NewRegistry()
	max := int64(3)
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.print.design",
		Label:       "Print design",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
		Validation:  domainext.Validation{Max: &max},
	}); err != nil {
		t.Fatal(err)
	}
	values := extensionapp.NewValueService(reg, newCartTestExtensionValueRepo())
	cartSvc, carts, prices := newStorefrontCartServiceWith(values, nil)
	prices.set("var-1", "EUR", 1500)
	engine := createTestTheme(t)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()).WithCart(nil, cartSvc).WithExtensions(values)
	before := carts.count()

	unknown := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items",
		`{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"missing.field","value":"x"}]}`, storefrontJSONCartCall{})
	if unknown.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, unknown) != "unknown_field_code" {
		t.Fatalf("unknown field status=%d code=%s body=%s", unknown.Code, storefrontJSONErrorCode(t, unknown), unknown.Body.String())
	}
	if _, ok := findCookie(unknown, "shopanda_storefront_cart"); ok {
		t.Fatal("unknown field must not mint a cart cookie")
	}
	if carts.count() != before {
		t.Fatalf("unknown field leftover carts = %d, want %d", carts.count(), before)
	}

	tooLong := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items",
		`{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"acme.print.design","value":"toolong"}]}`, storefrontJSONCartCall{})
	if tooLong.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, tooLong) != "field_validation_failed" {
		t.Fatalf("field validation status=%d code=%s body=%s", tooLong.Code, storefrontJSONErrorCode(t, tooLong), tooLong.Body.String())
	}
	if _, ok := findCookie(tooLong, "shopanda_storefront_cart"); ok {
		t.Fatal("field validation must not mint a cart cookie")
	}
	if carts.count() != before {
		t.Fatalf("field validation leftover carts = %d, want %d", carts.count(), before)
	}

	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.print.secret",
		Label:       "Internal design id",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
		Visibility:  domainext.VisibilityPrivate,
	}); err != nil {
		t.Fatal(err)
	}
	private := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items",
		`{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"acme.print.secret","value":"x"}]}`, storefrontJSONCartCall{})
	if private.Code != http.StatusForbidden || storefrontJSONErrorCode(t, private) != "forbidden_private_field" {
		t.Fatalf("private field status=%d code=%s body=%s", private.Code, storefrontJSONErrorCode(t, private), private.Body.String())
	}
	if _, ok := findCookie(private, "shopanda_storefront_cart"); ok {
		t.Fatal("private field must not mint a cart cookie")
	}
	if carts.count() != before {
		t.Fatalf("private field leftover carts = %d, want %d", carts.count(), before)
	}

	okRec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items",
		`{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"acme.print.design","value":"ok"}]}`, storefrontJSONCartCall{})
	if okRec.Code != http.StatusOK {
		t.Fatalf("valid extension status=%d body=%s", okRec.Code, okRec.Body.String())
	}
	if carts.count() != before+1 {
		t.Fatalf("success carts = %d, want %d (first persisted cart)", carts.count(), before+1)
	}
}

func TestStorefrontHandler_JSONCartAdd_ExistingEmptyGuestCartNotDiscarded(t *testing.T) {
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.print.design",
		Label:       "Print design",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatal(err)
	}
	values := extensionapp.NewValueService(reg, newCartTestExtensionValueRepo())
	cartSvc, carts, prices := newStorefrontCartServiceWith(values, nil)
	prices.set("var-1", "EUR", 1500)
	guest, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	engine := createTestTheme(t)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()).WithCart(nil, cartSvc).WithExtensions(values)

	rec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items",
		`{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"missing.field","value":"x"}]}`,
		storefrontJSONCartCall{Cookies: []*http.Cookie{{Name: "shopanda_storefront_cart", Value: guest.ID}}})
	if rec.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, rec) != "unknown_field_code" {
		t.Fatalf("status=%d code=%s body=%s", rec.Code, storefrontJSONErrorCode(t, rec), rec.Body.String())
	}
	got, err := carts.FindByID(context.Background(), guest.ID)
	if err != nil || got == nil {
		t.Fatalf("existing empty guest cart was discarded: got=%v err=%v", got, err)
	}
	if carts.count() != 1 {
		t.Fatalf("carts = %d, want 1", carts.count())
	}
}

type failDeleteCartRepo struct {
	*storefrontCartRepoStub
	err error
}

func (r *failDeleteCartRepo) Delete(_ context.Context, _ string) error {
	if r.err != nil {
		return r.err
	}
	return fmt.Errorf("delete failed")
}

func TestStorefrontHandler_JSONCartAdd_DiscardDeleteFailureLogs(t *testing.T) {
	carts := &failDeleteCartRepo{storefrontCartRepoStub: newStorefrontCartRepoStub(), err: fmt.Errorf("delete denied")}
	prices := newStorefrontPriceRepoStub()
	prices.set("var-1", "EUR", 1500)
	hookReg := hooks.NewRegistry(nil)
	if err := hookReg.Register(hooks.HookCartValidate, 100, "plugin.test", func(ctx *hooks.Context) error {
		hooks.AppendValidationIssue(ctx, extapi.CartValidationIssue{Code: "acme.blocked", Message: "not allowed"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svcLog := logger.NewWithWriter(io.Discard, "error")
	pipeline := pricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), pricing.NewFinalizeStep())
	vr, pr := cartApp.PermissiveCatalogRepos()
	cartSvc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, svcLog, event.NewBus(svcLog), nil, hookReg)
	engine := createTestTheme(t)
	var logs bytes.Buffer
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()).
		WithCart(nil, cartSvc).
		WithLog(logger.NewWithWriter(&logs, "error"))

	rec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":1}`, storefrontJSONCartCall{})
	if rec.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, rec) != "cart_validation_failed" {
		t.Fatalf("status=%d code=%s body=%s", rec.Code, storefrontJSONErrorCode(t, rec), rec.Body.String())
	}
	if _, ok := findCookie(rec, "shopanda_storefront_cart"); ok {
		t.Fatal("failed discard must not mint a cart cookie")
	}
	if carts.count() != 1 {
		t.Fatalf("leftover carts = %d, want 1 after delete failure", carts.count())
	}
	if !strings.Contains(logs.String(), "storefront.cart.json.add.discard_failed") {
		t.Fatalf("missing discard_failed log: %s", logs.String())
	}
}

func TestStorefrontHandler_JSONCartAdd_BlockedByValidateHook(t *testing.T) {
	hookReg := hooks.NewRegistry(nil)
	if err := hookReg.Register(hooks.HookCartValidate, 100, "plugin.test", func(ctx *hooks.Context) error {
		hooks.AppendValidationIssue(ctx, extapi.CartValidationIssue{Code: "acme.blocked", Message: "not allowed"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cartSvc, carts, prices := newStorefrontCartServiceWith(nil, hookReg)
	prices.set("var-1", "EUR", 1500)
	engine := createTestTheme(t)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()).WithCart(nil, cartSvc)
	before := carts.count()

	rec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":1}`, storefrontJSONCartCall{})
	if rec.Code != http.StatusUnprocessableEntity || storefrontJSONErrorCode(t, rec) != "cart_validation_failed" {
		t.Fatalf("status=%d code=%s body=%s", rec.Code, storefrontJSONErrorCode(t, rec), rec.Body.String())
	}
	if _, ok := findCookie(rec, "shopanda_storefront_cart"); ok {
		t.Fatal("blocked validate hook must not mint a cart cookie")
	}
	if carts.count() != before {
		t.Fatalf("hook leftover carts = %d, want %d", carts.count(), before)
	}
	if strings.Contains(rec.Body.String(), "cart_id") {
		t.Fatal("validation failure must not expose cart_id")
	}
	env := parseStorefrontJSON(t, rec)
	data, _ := env["data"].(map[string]interface{})
	if data == nil || data["validation_errors"] == nil {
		t.Fatalf("expected validation_errors, got %s", rec.Body.String())
	}
}

func TestStorefrontHandler_JSONCart_SignedInUsesCustomerCart(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	prices.set("var-2", "EUR", 1500)
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

	guestCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cartSvc.AddItem(context.Background(), guestCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatal(err)
	}
	custCart, err := cartSvc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatal(err)
	}

	addRec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-2","quantity":2}`, storefrontJSONCartCall{
		CustomerID: "cust-1",
		Cookies:    []*http.Cookie{{Name: "shopanda_storefront_cart", Value: guestCart.ID}},
	})
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", addRec.Code, addRec.Body.String())
	}
	if _, ok := findCookie(addRec, "shopanda_storefront_cart"); ok {
		t.Fatal("signed-in add must not set a guest cart cookie")
	}

	gotCust, err := carts.FindByID(context.Background(), custCart.ID)
	if err != nil || gotCust == nil || gotCust.TotalQuantity() != 2 {
		t.Fatalf("customer cart qty = %+v err=%v", gotCust, err)
	}
	gotGuest, err := carts.FindByID(context.Background(), guestCart.ID)
	if err != nil || gotGuest == nil || gotGuest.TotalQuantity() != 1 {
		t.Fatalf("guest cart should be unchanged, got %+v err=%v", gotGuest, err)
	}

	updRec := storefrontJSONCartRequest(t, h, http.MethodPut, "/storefront/cart/items/var-2", `{"quantity":4}`, storefrontJSONCartCall{
		CustomerID: "cust-1",
		Cookies:    []*http.Cookie{{Name: "shopanda_storefront_cart", Value: guestCart.ID}},
	})
	if updRec.Code != http.StatusOK || !strings.Contains(updRec.Body.String(), `"total_quantity":4`) {
		t.Fatalf("update status=%d body=%s", updRec.Code, updRec.Body.String())
	}

	delRec := storefrontJSONCartRequest(t, h, http.MethodDelete, "/storefront/cart/items/var-2", "", storefrontJSONCartCall{
		CustomerID: "cust-1",
		Cookies:    []*http.Cookie{{Name: "shopanda_storefront_cart", Value: guestCart.ID}},
	})
	if delRec.Code != http.StatusOK || !strings.Contains(delRec.Body.String(), `"total_quantity":0`) {
		t.Fatalf("remove status=%d body=%s", delRec.Code, delRec.Body.String())
	}
	gotGuest, _ = carts.FindByID(context.Background(), guestCart.ID)
	if gotGuest.TotalQuantity() != 1 {
		t.Fatalf("guest cart mutated: qty=%d", gotGuest.TotalQuantity())
	}
}

func TestStorefrontHandler_JSONCartAdd_UnexpectedErrorIsGeneric500(t *testing.T) {
	carts := &failExistingSaveRepo{storefrontCartRepoStub: newStorefrontCartRepoStub()}
	prices := newStorefrontPriceRepoStub()
	prices.set("var-1", "EUR", 1500)
	svcLog := logger.NewWithWriter(io.Discard, "error")
	pipeline := pricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), pricing.NewFinalizeStep())
	vr, pr := cartApp.PermissiveCatalogRepos()
	cartSvc := cartApp.NewService(carts, prices, nil, nil, vr, pr, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock(), pipeline, svcLog, event.NewBus(svcLog), nil, nil)
	engine := createTestTheme(t)
	var logs bytes.Buffer
	h := storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()).
		WithCart(nil, cartSvc).
		WithLog(logger.NewWithWriter(&logs, "error"))
	before := carts.count()

	rec := storefrontJSONCartRequest(t, h, http.MethodPost, "/storefront/cart/items", `{"variant_id":"var-1","quantity":1}`, storefrontJSONCartCall{})
	if rec.Code != http.StatusInternalServerError || storefrontJSONErrorCode(t, rec) != "internal" {
		t.Fatalf("status=%d code=%s body=%s", rec.Code, storefrontJSONErrorCode(t, rec), rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "cart_id") || strings.Contains(body, "disk full") {
		t.Fatalf("500 must not leak internals: %s", body)
	}
	if _, ok := findCookie(rec, "shopanda_storefront_cart"); ok {
		t.Fatal("unexpected add failure must not mint a cart cookie")
	}
	if carts.count() != before {
		t.Fatalf("500 leftover carts = %d, want %d", carts.count(), before)
	}
	if !strings.Contains(logs.String(), "storefront.cart.json.add.failed") {
		t.Fatalf("missing add.failed log: %s", logs.String())
	}
}

func findCookie(rec *httptest.ResponseRecorder, name string) (*http.Cookie, bool) {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c, true
		}
	}
	return nil, false
}
