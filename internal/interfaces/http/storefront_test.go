package http_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- mock repo for storefront tests ---

type mockStorefrontRepo struct {
	findByIDFn   func(ctx context.Context, id string) (*catalog.Product, error)
	findBySlugFn func(ctx context.Context, slug string) (*catalog.Product, error)
}

type mockStorefrontCategoryRepo struct {
	findBySlugFn func(ctx context.Context, slug string) (*catalog.Category, error)
	findAllFn    func(ctx context.Context) ([]catalog.Category, error)
}

func (m *mockStorefrontRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockStorefrontRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockStorefrontRepo) List(_ context.Context, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockStorefrontRepo) Create(_ context.Context, _ *catalog.Product) error { return nil }
func (m *mockStorefrontRepo) Update(_ context.Context, _ *catalog.Product) error { return nil }
func (m *mockStorefrontRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockStorefrontRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

func (m *mockStorefrontCategoryRepo) FindByID(_ context.Context, _ string) (*catalog.Category, error) {
	return nil, nil
}

func (m *mockStorefrontCategoryRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Category, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockStorefrontCategoryRepo) FindByParentID(_ context.Context, _ *string) ([]catalog.Category, error) {
	return nil, nil
}

func (m *mockStorefrontCategoryRepo) FindAll(ctx context.Context) ([]catalog.Category, error) {
	if m.findAllFn != nil {
		return m.findAllFn(ctx)
	}
	return []catalog.Category{}, nil
}

func (m *mockStorefrontCategoryRepo) Create(_ context.Context, _ *catalog.Category) error { return nil }

func (m *mockStorefrontCategoryRepo) Update(_ context.Context, _ *catalog.Category) error { return nil }

// --- test theme helpers ---

func createTestTheme(t *testing.T) *theme.Engine {
	t.Helper()
	dir := t.TempDir()

	// theme.yaml
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\nstorefront:\n  search_action: /catalog\n  cart_url: /basket\n  cart_label: Basket (2)\n  nav:\n    - label: Start\n      url: /\n    - label: Browse\n      url: /categories\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// templates directory
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}

	layout := `<!DOCTYPE html><html><head><title>{{ template "title" . }}</title></head><body><nav>{{ range .Layout.Nav }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}</nav><form action="{{ .Layout.SearchAction }}"></form><a href="{{ .Layout.CartURL }}">{{ if .Layout.EnableCart }}<span hx-get="/fragments/cart-count" hx-trigger="cart-updated from:body" hx-swap="innerHTML">{{ .Layout.CartLabel }}</span>{{ else }}{{ .Layout.CartLabel }}{{ end }}</a>{{ if .Layout.EnableCart }}<div id="mini-cart" hx-get="/fragments/mini-cart" hx-trigger="load, cart-updated from:body"></div>{{ end }}{{ template "content" . }}</body></html>`
	if err := os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}

	home := `{{ define "title" }}{{ .Layout.SiteName }}{{ end }}{{ define "content" }}<h1>Welcome to {{ .Layout.SiteName }}</h1>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "home.html"), []byte(home), 0644); err != nil {
		t.Fatal(err)
	}

	product := `{{ define "title" }}{{ .Product.Name }}{{ end }}{{ define "content" }}<h1>{{ .Product.Name }}</h1><p>{{ .Product.Description }}</p>{{ if .CartForm }}<form action="{{ .CartForm.Action }}" method="post"><input type="hidden" name="variant_id" value="{{ .CartForm.VariantID }}"><input type="hidden" name="quantity" value="{{ .CartForm.Quantity }}"><input type="hidden" name="redirect_to" value="{{ .CartForm.RedirectTo }}"><button type="submit">Add to cart</button></form>{{ end }}{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product.html"), []byte(product), 0644); err != nil {
		t.Fatal(err)
	}

	listing := `{{ define "title" }}{{ .Title }}{{ end }}{{ define "content" }}<h1>{{ .Title }}</h1><p>{{ .ResultSummary }}</p><div class="view-{{ .View }}">{{ range .Products }}<article><a href="/products/{{ .Slug }}">{{ .Name }}</a><span>{{ .PriceText }}</span><small>{{ .Availability }}</small></article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}</div><nav>{{ range .SortOptions }}{{ if .Selected }}<strong>{{ .Label }}</strong>{{ else }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}{{ end }}</nav><div>{{ range .Pagination.Links }}{{ if .Current }}<strong>{{ .Label }}</strong>{{ else }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}{{ end }}</div>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product_list.html"), []byte(listing), 0644); err != nil {
		t.Fatal(err)
	}

	category := `{{ define "title" }}{{ .Category.Name }}{{ end }}{{ define "content" }}<h1>{{ .Category.Name }}</h1><p>{{ .Category.Description }}</p><nav>{{ range .Breadcrumbs }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}</nav><section>{{ range .Subcategories }}<a href="{{ .URL }}">{{ .Name }}</a>{{ end }}</section><div>{{ range .Products }}<article>{{ .Name }}</article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}</div>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "category.html"), []byte(category), 0644); err != nil {
		t.Fatal(err)
	}

	cart := `{{ define "title" }}Cart{{ end }}{{ define "content" }}<section id="cart-page"><h1>Shopping Cart</h1>{{ range .Items }}<article><h2>{{ .ProductName }}</h2><span>{{ .UnitPriceText }}</span><form action="/cart/update" method="post"><input type="hidden" name="variant_id" value="{{ .VariantID }}"><input type="number" name="quantity" value="{{ .Quantity }}"></form><form action="/cart/remove" method="post"><input type="hidden" name="variant_id" value="{{ .VariantID }}"><button type="submit">Remove</button></form><strong>{{ .LineTotalText }}</strong></article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}<div>{{ .Summary.SubtotalText }}</div></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "cart.html"), []byte(cart), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutAddress := `{{ define "title" }}Checkout: Address{{ end }}{{ define "content" }}<section><h1>Checkout</h1>{{ if .RequiresAuth }}<p>Sign in to continue checkout.</p>{{ else }}<form action="/checkout/shipping" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="first_name" value="{{ .Address.FirstName }}"><input name="last_name" value="{{ .Address.LastName }}"><input name="street" value="{{ .Address.Street }}"><input name="city" value="{{ .Address.City }}"><input name="postcode" value="{{ .Address.Postcode }}"><select name="country">{{ range .Countries }}<option value="{{ .Value }}" {{ if .Selected }}selected{{ end }}>{{ .Label }}</option>{{ end }}</select><button type="submit">Continue to Shipping</button></form>{{ end }}{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}</section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "checkout_address.html"), []byte(checkoutAddress), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutShipping := `{{ define "title" }}Checkout: Shipping{{ end }}{{ define "content" }}<section><h1>Shipping</h1>{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<form action="/checkout/payment" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="first_name" value="{{ .Address.FirstName }}"><input type="hidden" name="last_name" value="{{ .Address.LastName }}"><input type="hidden" name="street" value="{{ .Address.Street }}"><input type="hidden" name="city" value="{{ .Address.City }}"><input type="hidden" name="postcode" value="{{ .Address.Postcode }}"><input type="hidden" name="country" value="{{ .Address.Country }}">{{ range .Rates }}<label><input type="radio" name="shipping_method" value="{{ .Method }}" {{ if .Selected }}checked{{ end }}>{{ .Label }} — {{ .CostText }}</label>{{ end }}<button type="submit">Continue to Payment</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "checkout_shipping.html"), []byte(checkoutShipping), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutPayment := `{{ define "title" }}Checkout: Payment{{ end }}{{ define "content" }}<section><h1>Payment</h1>{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<form action="/checkout/confirm" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="first_name" value="{{ .Address.FirstName }}"><input type="hidden" name="last_name" value="{{ .Address.LastName }}"><input type="hidden" name="street" value="{{ .Address.Street }}"><input type="hidden" name="city" value="{{ .Address.City }}"><input type="hidden" name="postcode" value="{{ .Address.Postcode }}"><input type="hidden" name="country" value="{{ .Address.Country }}"><input type="hidden" name="shipping_method" value="{{ if .SelectedRate }}{{ .SelectedRate.Method }}{{ end }}"><input type="hidden" name="payment_method" value="{{ .Payment.Method }}"><p>{{ .Payment.Label }}</p><button type="submit">Place Order</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "checkout_payment.html"), []byte(checkoutPayment), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutConfirm := `{{ define "title" }}Checkout: Confirm{{ end }}{{ define "content" }}<section><h1>Order Placed</h1>{{ if .Confirmation }}<p>Order #{{ .Confirmation.OrderID }}</p><p>{{ .Confirmation.TotalText }}</p><p>{{ .Confirmation.Notice }}</p>{{ end }}</section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "checkout_confirm.html"), []byte(checkoutConfirm), 0644); err != nil {
		t.Fatal(err)
	}

	accountLogin := `{{ define "title" }}Account Login{{ end }}{{ define "content" }}<section><h1>Sign in</h1>{{ if .SuccessMessage }}<p>{{ .SuccessMessage }}</p>{{ end }}{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<form action="/account/login" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="redirect_to" value="{{ .RedirectTo }}"><input name="email" value="{{ .Email }}"><input name="password" type="password"><button type="submit">Login</button></form><a href="/account/register">Register</a></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_login.html"), []byte(accountLogin), 0644); err != nil {
		t.Fatal(err)
	}

	accountRegister := `{{ define "title" }}Account Register{{ end }}{{ define "content" }}<section><h1>Create account</h1>{{ if .SuccessMessage }}<p>{{ .SuccessMessage }}</p>{{ end }}{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<form action="/account/register" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="redirect_to" value="{{ .RedirectTo }}"><input name="first_name" value="{{ .FirstName }}"><input name="last_name" value="{{ .LastName }}"><input name="email" value="{{ .Email }}"><input name="password" type="password"><button type="submit">Create Account</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_register.html"), []byte(accountRegister), 0644); err != nil {
		t.Fatal(err)
	}

	accountOrders := `{{ define "title" }}Account Orders{{ end }}{{ define "content" }}<section><h1>Your Orders</h1>{{ range .Orders }}<article><a href="{{ .URL }}">{{ .ID }}</a><span>{{ .DateText }}</span><strong>{{ .TotalText }}</strong><em>{{ .Status }}</em></article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}</section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_orders.html"), []byte(accountOrders), 0644); err != nil {
		t.Fatal(err)
	}

	accountOrderDetail := `{{ define "title" }}Account Order{{ end }}{{ define "content" }}<section><h1>Order {{ .OrderID }}</h1><p>{{ .Status }}</p><p>{{ .TotalText }}</p>{{ range .Items }}<article><strong>{{ .Name }}</strong><span>{{ .Quantity }}</span><span>{{ .LineTotalText }}</span></article>{{ end }}<a href="{{ .BackURL }}">Back</a></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_order_detail.html"), []byte(accountOrderDetail), 0644); err != nil {
		t.Fatal(err)
	}

	accountProfile := `{{ define "title" }}Account Profile{{ end }}{{ define "content" }}<section><h1>Profile</h1>{{ if .SuccessMessage }}<p>{{ .SuccessMessage }}</p>{{ end }}{{ if .ProfileErrorMessage }}<p>{{ .ProfileErrorMessage }}</p>{{ end }}{{ if .PasswordErrorMessage }}<p>{{ .PasswordErrorMessage }}</p>{{ end }}<p>{{ .Email }}</p><form action="/account/profile" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="first_name" value="{{ .FirstName }}"><input name="last_name" value="{{ .LastName }}"><button type="submit">Save</button></form><form action="/account/profile/password" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="current_password" type="password"><input name="new_password" type="password"><button type="submit">Change Password</button></form><form action="/account/profile/delete" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><button type="submit">Delete Account</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_profile.html"), []byte(accountProfile), 0644); err != nil {
		t.Fatal(err)
	}

	engine, err := theme.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func createTestThemeWithoutHome(t *testing.T) *theme.Engine {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}

	layout := `<!DOCTYPE html><html><head><title>{{ template "title" . }}</title></head><body>{{ template "content" . }}</body></html>`
	if err := os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}

	product := `{{ define "title" }}{{ .Product.Name }}{{ end }}{{ define "content" }}<h1>{{ .Product.Name }}</h1>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product.html"), []byte(product), 0644); err != nil {
		t.Fatal(err)
	}

	engine, err := theme.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func newStorefrontRouter(h *shophttp.StorefrontHandler) http.Handler {
	router := shophttp.NewRouter()
	router.Use(shophttp.CSRFMiddleware())
	router.HandleFunc("GET /{$}", h.Home())
	router.HandleFunc("GET /account/login", h.Login())
	router.HandleFunc("POST /account/login", h.Login())
	router.HandleFunc("GET /account/register", h.Register())
	router.HandleFunc("POST /account/register", h.Register())
	router.HandleFunc("POST /account/logout", h.Logout())
	router.HandleFunc("GET /account/orders", h.AccountOrders())
	router.HandleFunc("GET /account/orders/{orderId}", h.AccountOrderDetail())
	router.HandleFunc("GET /account/profile", h.AccountProfile())
	router.HandleFunc("POST /account/profile", h.AccountProfile())
	router.HandleFunc("POST /account/profile/password", h.AccountPassword())
	router.HandleFunc("POST /account/profile/delete", h.AccountDelete())
	router.HandleFunc("GET /cart", h.Cart())
	router.HandleFunc("GET /checkout/address", h.CheckoutAddress())
	router.HandleFunc("GET /checkout/shipping", h.CheckoutShipping())
	router.HandleFunc("POST /checkout/shipping", h.CheckoutShipping())
	router.HandleFunc("GET /checkout/payment", h.CheckoutPayment())
	router.HandleFunc("POST /checkout/payment", h.CheckoutPayment())
	router.HandleFunc("GET /checkout/confirm", h.CheckoutConfirm())
	router.HandleFunc("POST /checkout/confirm", h.CheckoutConfirm())
	router.HandleFunc("GET /categories", h.Categories())
	router.HandleFunc("GET /categories/{slug}", h.Category())
	router.HandleFunc("GET /fragments/cart-count", h.CartCountFragment())
	router.HandleFunc("GET /fragments/mini-cart", h.MiniCartFragment())
	router.HandleFunc("GET /products", h.Products())
	router.HandleFunc("GET /products/{slug}", h.Product())
	router.HandleFunc("POST /cart/add", h.AddToCart())
	router.HandleFunc("POST /cart/update", h.UpdateCart())
	router.HandleFunc("POST /cart/remove", h.RemoveCartItem())
	router.HandleFunc("POST /fragments/cart/add", h.AddToCart())
	router.HandleFunc("POST /fragments/cart/update", h.UpdateCart())
	router.HandleFunc("POST /fragments/cart/remove", h.RemoveCartItem())
	router.HandleFunc("GET /search", h.Search())
	return router.Handler()
}

func newStorefrontSearchMock() *mockSearchEngine {
	return &mockSearchEngine{searchFn: func(_ context.Context, _ search.SearchQuery) (search.SearchResult, error) {
		return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}, nil
	}}
}

func newStorefrontCategoryMock() *mockStorefrontCategoryRepo {
	return &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return []catalog.Category{}, nil
	}}
}

// --- tests ---

func TestStorefrontHandler_Product_OK(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{
				ID:          "p1",
				Name:        "Widget",
				Slug:        slug,
				Description: "A fine widget",
			}, nil
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<h1>Widget</h1>") {
		t.Errorf("body missing product name heading; got: %s", body)
	}
	if !strings.Contains(body, "A fine widget") {
		t.Errorf("body missing description; got: %s", body)
	}
}

func TestStorefrontHandler_Home_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Welcome to test") {
		t.Fatalf("body missing home welcome text: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Basket (2)") {
		t.Fatalf("body missing configured cart label: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_Home_MissingTemplate(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestThemeWithoutHome(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestStorefrontRouter_Home_DoesNotCatchUnknownPath(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestStorefrontHandler_Category_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	parentID := "cat-root"
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return []catalog.Category{
			{ID: "cat-root", Name: "Electronics", Slug: "electronics", Meta: map[string]interface{}{"description": "Devices and gadgets"}},
			{ID: "cat-child", ParentID: &parentID, Name: "Headphones", Slug: "headphones", Meta: map[string]interface{}{"description": "Over-ear and in-ear"}},
			{ID: "cat-grandchild", ParentID: stringPtr("cat-child"), Name: "Wireless", Slug: "wireless"},
		}, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Filters["category"] != "cat-child" {
			t.Fatalf("category filter = %v, want cat-child", query.Filters["category"])
		}
		return search.SearchResult{Products: []search.Product{{ID: "p-1", Name: "Studio Headset", Slug: "studio-headset"}}, Total: 1, Facets: map[string][]search.FacetValue{}}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/categories/headphones", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Headphones") {
		t.Fatalf("body missing category title: %s", body)
	}
	if !strings.Contains(body, "Electronics") {
		t.Fatalf("body missing breadcrumb/root category context: %s", body)
	}
	if !strings.Contains(body, "Wireless") {
		t.Fatalf("body missing subcategory link: %s", body)
	}
	if !strings.Contains(body, "Studio Headset") {
		t.Fatalf("body missing category product: %s", body)
	}
}

func TestStorefrontHandler_Categories_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return []catalog.Category{{ID: "cat-1", Name: "Electronics", Slug: "electronics"}, {ID: "cat-2", Name: "Clothing", Slug: "clothing"}}, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if len(query.Filters) != 0 {
			t.Fatalf("expected no category filter, got %v", query.Filters)
		}
		return search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/categories", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Categories") {
		t.Fatalf("body missing categories title: %s", body)
	}
	if !strings.Contains(body, "Electronics") || !strings.Contains(body, "Clothing") {
		t.Fatalf("body missing root category links: %s", body)
	}
}

func stringPtr(v string) *string {
	return &v
}

func TestStorefrontHandler_Category_NotFound(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/categories/missing", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestStorefrontHandler_Home_CategoryRepoError_Degrades(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return nil, errors.New("db down")
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestStorefrontHandler_LayoutCategoryCache(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	findAllCalls := 0
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		findAllCalls++
		return []catalog.Category{{ID: "cat-1", Name: "Electronics", Slug: "electronics"}}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, newStorefrontSearchMock())

	for _, path := range []string{"/", "/products"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		newStorefrontRouter(h).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("path %s status = %d, want %d; body: %s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	if findAllCalls != 1 {
		t.Fatalf("FindAll calls = %d, want 1", findAllCalls)
	}
}

func TestStorefrontHandler_Products_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	plp.AddStep(addListingBlockStep{name: "listing", typ: "product_grid"})
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Text != "" {
			t.Fatalf("query.Text = %q, want empty", query.Text)
		}
		if query.Limit != 12 {
			t.Fatalf("query.Limit = %d, want 12", query.Limit)
		}
		if query.Offset != 0 {
			t.Fatalf("query.Offset = %d, want 0", query.Offset)
		}
		if query.Sort != "-created_at" {
			t.Fatalf("query.Sort = %q, want -created_at", query.Sort)
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Widget", Slug: "widget", Attributes: map[string]interface{}{"image_url": "/media/widget.jpg"}}},
			Total:    1,
			Facets:   map[string][]search.FacetValue{"category": []search.FacetValue{{Value: "Shoes", Count: 1}}},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "All Products") {
		t.Fatalf("body missing listing title: %s", body)
	}
	if !strings.Contains(body, "Widget") {
		t.Fatalf("body missing product card: %s", body)
	}
	if !strings.Contains(body, "Newest") {
		t.Fatalf("body missing sort option label: %s", body)
	}
}

func TestStorefrontHandler_Search_OK(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Text != "boots" {
			t.Fatalf("query.Text = %q, want boots", query.Text)
		}
		if query.Sort != "name" {
			t.Fatalf("query.Sort = %q, want name", query.Sort)
		}
		if query.Limit != 24 {
			t.Fatalf("query.Limit = %d, want 24", query.Limit)
		}
		if query.Offset != 24 {
			t.Fatalf("query.Offset = %d, want 24", query.Offset)
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Trail Boot", Slug: "trail-boot", Price: 12999, InStock: true}},
			Total:    26,
			Facets:   map[string][]search.FacetValue{},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/search?q=boots&sort=name_asc&page=2&per_page=24&view=list", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Search results for &#34;boots&#34;") {
		t.Fatalf("body missing search title: %s", body)
	}
	if !strings.Contains(body, "Trail Boot") {
		t.Fatalf("body missing search product: %s", body)
	}
	if !strings.Contains(body, "EUR 129.99") {
		t.Fatalf("body missing formatted price: %s", body)
	}
	if !strings.Contains(body, "In stock") {
		t.Fatalf("body missing availability text: %s", body)
	}
}

func TestStorefrontHandler_Products_InvalidPagination(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products?page=0", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestStorefrontHandler_Product_NotFound(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return nil, nil
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/nonexistent", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStorefrontHandler_Product_NotFoundError(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return nil, apperror.NotFound("product not found")
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/gone", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStorefrontHandler_Product_RepoError(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return nil, apperror.Internal("db down")
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestStorefrontHandler_Product_WithPipeline(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug}, nil
		},
	}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(addBlockStep{name: "hero", typ: "hero_banner"})
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	// Pipeline ran but template still renders product name.
	if !strings.Contains(rec.Body.String(), "Widget") {
		t.Errorf("body missing product name")
	}
}
