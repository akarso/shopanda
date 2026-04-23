package http_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/composition"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

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

func (m *mockStorefrontVariantRepo) ListByProductID(ctx context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	if m.listByProductIDFn != nil {
		return m.listByProductIDFn(ctx, productID, offset, limit)
	}
	return nil, nil
}

func (m *mockStorefrontVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockStorefrontVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

func newStorefrontCartService() (*cartApp.Service, *storefrontCartRepoStub, *storefrontPriceRepoStub) {
	carts := newStorefrontCartRepoStub()
	prices := newStorefrontPriceRepoStub()
	log := logger.NewWithWriter(io.Discard, "error")
	pipeline := pricing.NewPipeline(appPricing.NewBasePriceStep(prices), pricing.NewFinalizeStep())
	service := cartApp.NewService(carts, prices, nil, nil, pipeline, log, event.NewBus(log))
	return service, carts, prices
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
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

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
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

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
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

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
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithLog(logger.NewWithWriter(&logs, "error"))

	form := url.Values{"variant_id": {"var-1"}, "quantity": {"1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cart/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Request-ID", "req-123")
	handler := shophttp.RequestIDMiddleware()(newStorefrontRouter(h))
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
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(nil, cartSvc)

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
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 2); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

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
	_, _ = cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1)
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

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
	_, _ = cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1)
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc)

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
