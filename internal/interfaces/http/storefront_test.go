package http_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/akarso/shopanda/internal/testutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/theme"
	themeapp "github.com/akarso/shopanda/internal/application/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type stubLegalConfig map[string]interface{}

func (s stubLegalConfig) Get(_ context.Context, key string) (interface{}, error) {
	if s == nil {
		return nil, nil
	}
	v, ok := s[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

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

func (m *mockStorefrontCategoryRepo) Delete(_ context.Context, _ string) error { return nil }

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

	layout := `<!DOCTYPE html><html><head><title>{{ template "title" . }}</title></head><body><nav>{{ range .Layout.Nav }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}</nav><form action="{{ .Layout.SearchAction }}"></form><div class="account-widget"><a href="{{ .Layout.AccountURL }}">{{ .Layout.AccountLabel }}</a>{{ if .Layout.AccountSignedIn }}<strong>{{ .Layout.AccountName }}</strong><a href="{{ .Layout.AccountProfileURL }}">Profile</a><a href="{{ .Layout.AccountOrdersURL }}">Orders</a><a href="{{ .Layout.AccountSecurityURL }}">Security</a><form action="{{ .Layout.AccountLogoutURL }}" method="post"><input type="hidden" name="csrf_token" value="{{ .Layout.CSRFToken }}"><button type="submit">Log out</button></form>{{ else }}<span>Sign in to view orders and profile.</span>{{ end }}</div><a href="{{ .Layout.CartURL }}">{{ if .Layout.EnableCart }}<span hx-get="/fragments/cart-count" hx-trigger="cart-updated from:body" hx-swap="innerHTML">{{ .Layout.CartLabel }}</span>{{ else }}{{ .Layout.CartLabel }}{{ end }}</a>{{ if .Layout.EnableCart }}<div id="mini-cart" hx-get="/fragments/mini-cart" hx-trigger="load, cart-updated from:body"></div>{{ end }}{{ template "content" . }}</body></html>`
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

	listing := `{{ define "title" }}{{ .Title }}{{ end }}{{ define "content" }}<h1>{{ .Title }}</h1><p>{{ .ResultSummary }}</p>{{ if .Filters }}<aside>{{ range .Filters }}{{ range .Values }}<a href="{{ .URL }}"{{ if .Selected }} data-selected="true"{{ end }}>{{ .Label }}</a>{{ end }}{{ end }}</aside>{{ end }}<div class="view-{{ .View }}">{{ range .Products }}<article><a href="/products/{{ .Slug }}">{{ .Name }}</a><span>{{ .PriceText }}</span><small>{{ .Availability }}</small></article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}</div><nav>{{ range .SortOptions }}{{ if .Selected }}<strong>{{ .Label }}</strong>{{ else }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}{{ end }}</nav><div>{{ range .Pagination.Links }}{{ if .Current }}<strong>{{ .Label }}</strong>{{ else }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}{{ end }}</div>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product_list.html"), []byte(listing), 0644); err != nil {
		t.Fatal(err)
	}

	category := `{{ define "title" }}{{ .Category.Name }}{{ end }}{{ define "content" }}<h1>{{ .Category.Name }}</h1><p>{{ .Category.Description }}</p><nav>{{ range .Breadcrumbs }}<a href="{{ .URL }}">{{ .Label }}</a>{{ end }}</nav><section>{{ range .Subcategories }}<a href="{{ .URL }}">{{ .Name }}</a>{{ end }}</section>{{ if .Filters }}<aside>{{ range .Filters }}{{ range .Values }}<a href="{{ .URL }}"{{ if .Selected }} data-selected="true"{{ end }}>{{ .Label }}</a>{{ end }}{{ end }}</aside>{{ end }}<div>{{ range .Products }}<article>{{ .Name }}</article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}</div>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "category.html"), []byte(category), 0644); err != nil {
		t.Fatal(err)
	}

	cart := `{{ define "title" }}Cart{{ end }}{{ define "content" }}<section id="cart-page"><h1>Shopping Cart</h1>{{ range .Items }}<article><h2>{{ .ProductName }}</h2><span>{{ .UnitPriceText }}</span><form action="/cart/update" method="post"><input type="hidden" name="variant_id" value="{{ .VariantID }}"><input type="number" name="quantity" value="{{ .Quantity }}"></form><form action="/cart/remove" method="post"><input type="hidden" name="variant_id" value="{{ .VariantID }}"><button type="submit">Remove</button></form><strong>{{ .LineTotalText }}</strong></article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}<div>{{ .Summary.SubtotalText }}</div></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "cart.html"), []byte(cart), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutAddress := `{{ define "title" }}Checkout: Address{{ end }}{{ define "content" }}<section><h1>Checkout</h1><form action="/checkout/shipping" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="contact_email" value="{{ .ContactEmail }}"><input name="first_name" value="{{ .Address.FirstName }}"><input name="last_name" value="{{ .Address.LastName }}"><input name="street" value="{{ .Address.Street }}"><input name="city" value="{{ .Address.City }}"><input name="postcode" value="{{ .Address.Postcode }}"><select name="country">{{ range .Countries }}<option value="{{ .Value }}" {{ if .Selected }}selected{{ end }}>{{ .Label }}</option>{{ end }}</select><button type="submit">Continue to Shipping</button></form>{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}</section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "checkout_address.html"), []byte(checkoutAddress), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutShipping := `{{ define "title" }}Checkout: Shipping{{ end }}{{ define "content" }}<section><h1>Shipping</h1>{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<form action="/checkout/payment" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="contact_email" value="{{ .ContactEmail }}"><input type="hidden" name="first_name" value="{{ .Address.FirstName }}"><input type="hidden" name="last_name" value="{{ .Address.LastName }}"><input type="hidden" name="street" value="{{ .Address.Street }}"><input type="hidden" name="city" value="{{ .Address.City }}"><input type="hidden" name="postcode" value="{{ .Address.Postcode }}"><input type="hidden" name="country" value="{{ .Address.Country }}">{{ range .Rates }}<label><input type="radio" name="shipping_method" value="{{ .Method }}" {{ if .Selected }}checked{{ end }}>{{ .Label }} — {{ .CostText }}</label>{{ end }}<button type="submit">Continue to Payment</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "checkout_shipping.html"), []byte(checkoutShipping), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutPayment := `{{ define "title" }}Checkout: Payment{{ end }}{{ define "content" }}<section><h1>Payment</h1>{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<form action="/checkout/confirm" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="contact_email" value="{{ .ContactEmail }}"><input type="hidden" name="first_name" value="{{ .Address.FirstName }}"><input type="hidden" name="last_name" value="{{ .Address.LastName }}"><input type="hidden" name="street" value="{{ .Address.Street }}"><input type="hidden" name="city" value="{{ .Address.City }}"><input type="hidden" name="postcode" value="{{ .Address.Postcode }}"><input type="hidden" name="country" value="{{ .Address.Country }}"><input type="hidden" name="shipping_method" value="{{ if .SelectedRate }}{{ .SelectedRate.Method }}{{ end }}"><input type="hidden" name="payment_method" value="{{ .Payment.Method }}"><p>{{ .Payment.Label }}</p><button type="submit">Place Order</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "checkout_payment.html"), []byte(checkoutPayment), 0644); err != nil {
		t.Fatal(err)
	}

	checkoutConfirm := `{{ define "title" }}Checkout: Confirm{{ end }}{{ define "content" }}<section><h1>Order Placed</h1>{{ if .Confirmation }}<p>Order #{{ .Confirmation.OrderID }}</p><p>{{ .Confirmation.TotalText }}</p><p>{{ .Confirmation.Notice }}</p>{{ if .Confirmation.GuestEmail }}<p id="guest-confirmation-email">Confirmation will be sent to {{ .Confirmation.GuestEmail }}</p>{{ end }}{{ if .Confirmation.ViewOrderURL }}<a id="view-order" href="{{ .Confirmation.ViewOrderURL }}">View Order</a>{{ end }}{{ end }}</section>{{ end }}{{ template "layout.html" . }}`
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

	accountVerifyEmail := `{{ define "title" }}Verify Email{{ end }}{{ define "content" }}<section><h1>Verify Email</h1>{{ if .SuccessMessage }}<p>{{ .SuccessMessage }}</p>{{ end }}{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<a href="{{ .ContinueURL }}">Continue</a></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_verify_email.html"), []byte(accountVerifyEmail), 0644); err != nil {
		t.Fatal(err)
	}

	accountOrders := `{{ define "title" }}Account Orders{{ end }}{{ define "content" }}<section><nav><a href="{{ .AccountNav.OrdersURL }}">Orders</a><a href="{{ .AccountNav.ProfileURL }}">Profile</a><a href="{{ .AccountNav.SecurityURL }}">Security</a></nav><h1>Your Orders</h1>{{ range .Orders }}<article><a href="{{ .URL }}">{{ .ID }}</a><span>{{ .DateText }}</span><strong>{{ .TotalText }}</strong><em>{{ .Status }}</em></article>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}</section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_orders.html"), []byte(accountOrders), 0644); err != nil {
		t.Fatal(err)
	}

	accountOrdersClaim := `{{ define "title" }}Claim Orders{{ end }}{{ define "content" }}<section><h1>Claim your orders</h1>{{ if .ErrorMessage }}<p class="error">{{ .ErrorMessage }}</p>{{ end }}{{ if .Orders }}<p>Orders for {{ .Email }}</p>{{ range .Orders }}<article><strong>{{ .ID }}</strong><span>{{ .DateText }}</span><span>{{ .TotalText }}</span><em>{{ .Status }}</em></article>{{ end }}<form action="/account/orders/claim" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="claim_token" value="{{ .ClaimToken }}"><input name="first_name" value="{{ .FirstName }}"><input name="last_name" value="{{ .LastName }}"><input name="password" type="password"><button type="submit">Create Account</button></form>{{ else if .EmptyMessage }}<p>{{ .EmptyMessage }}</p>{{ end }}</section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_orders_claim.html"), []byte(accountOrdersClaim), 0644); err != nil {
		t.Fatal(err)
	}

	accountOrderDetail := `{{ define "title" }}Account Order{{ end }}{{ define "content" }}<section><nav><a href="{{ .AccountNav.OrdersURL }}">Orders</a><a href="{{ .AccountNav.ProfileURL }}">Profile</a><a href="{{ .AccountNav.SecurityURL }}">Security</a></nav><h1>Order {{ .OrderID }}</h1><p>{{ .Status }}</p><p>{{ .TotalText }}</p>{{ range .Items }}<article><strong>{{ .Name }}</strong><span>{{ .Quantity }}</span><span>{{ .LineTotalText }}</span></article>{{ end }}<a href="{{ .BackURL }}">Back</a></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_order_detail.html"), []byte(accountOrderDetail), 0644); err != nil {
		t.Fatal(err)
	}

	accountProfile := `{{ define "title" }}Account Profile{{ end }}{{ define "content" }}<section><nav><a href="{{ .AccountNav.OrdersURL }}">Orders</a><a href="{{ .AccountNav.ProfileURL }}">Profile</a><a href="{{ .AccountNav.SecurityURL }}">Security</a></nav><h1>Profile</h1>{{ if .SuccessMessage }}<p>{{ .SuccessMessage }}</p>{{ end }}{{ if .ProfileErrorMessage }}<p>{{ .ProfileErrorMessage }}</p>{{ end }}<p>{{ .Email }}</p><form action="/account/profile" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="first_name" value="{{ .FirstName }}"><input name="last_name" value="{{ .LastName }}"><button type="submit">Save</button></form><a href="{{ .AccountNav.SecurityURL }}">Manage security</a></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_profile.html"), []byte(accountProfile), 0644); err != nil {
		t.Fatal(err)
	}

	accountAddresses := `{{ define "title" }}Account Addresses{{ end }}{{ define "content" }}<section><nav><a href="{{ .AccountNav.OrdersURL }}">Orders</a><a href="{{ .AccountNav.AddressesURL }}">Addresses</a><a href="{{ .AccountNav.PreferencesURL }}">Preferences</a></nav><h1>Addresses</h1>{{ if .SuccessMessage }}<p class="notice">{{ .SuccessMessage }}</p>{{ end }}{{ if .ErrorMessage }}<p class="error">{{ .ErrorMessage }}</p>{{ end }}{{ if .Addresses }}<ul>{{ range .Addresses }}<li id="address-{{ .ID }}">{{ if .IsDefault }}<span class="default">Default</span>{{ end }}<strong>{{ .Recipient }}</strong>{{ range .Lines }}<span>{{ . }}</span>{{ end }}<a href="{{ .EditURL }}">Edit</a><form action="/account/addresses/{{ .ID }}/default" method="post"><input type="hidden" name="csrf_token" value="{{ $.CSRFToken }}"><button>Make default</button></form><form action="/account/addresses/{{ .ID }}/delete" method="post"><input type="hidden" name="csrf_token" value="{{ $.CSRFToken }}"><button>Delete</button></form></li>{{ end }}</ul>{{ else }}<p>{{ .EmptyMessage }}</p>{{ end }}<h2>{{ .FormTitle }}</h2><form action="{{ .FormAction }}" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="label" value="{{ .Form.Label }}"><input name="recipient" value="{{ .Form.Recipient }}"><input name="street" value="{{ .Form.Street }}"><input name="city" value="{{ .Form.City }}"><input name="postcode" value="{{ .Form.Postcode }}"><select name="country">{{ range .Countries }}<option value="{{ .Value }}" {{ if .Selected }}selected{{ end }}>{{ .Label }}</option>{{ end }}</select><input type="checkbox" name="is_default" value="1" {{ if .Form.IsDefault }}checked{{ end }}><button type="submit">{{ .SubmitLabel }}</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_addresses.html"), []byte(accountAddresses), 0644); err != nil {
		t.Fatal(err)
	}

	accountPreferences := `{{ define "title" }}Account Preferences{{ end }}{{ define "content" }}<section><nav><a href="{{ .AccountNav.PreferencesURL }}">Preferences</a></nav><h1>Preferences</h1>{{ if .SuccessMessage }}<p class="notice">{{ .SuccessMessage }}</p>{{ end }}{{ if .ErrorMessage }}<p class="error">{{ .ErrorMessage }}</p>{{ end }}<form action="/account/preferences" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="checkbox" name="marketing" value="1" {{ if .Marketing }}checked{{ end }}><button type="submit">Save preferences</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_preferences.html"), []byte(accountPreferences), 0644); err != nil {
		t.Fatal(err)
	}

	accountSecurity := `{{ define "title" }}Account Security{{ end }}{{ define "content" }}<section><nav><a href="{{ .AccountNav.OrdersURL }}">Orders</a><a href="{{ .AccountNav.ProfileURL }}">Profile</a><a href="{{ .AccountNav.SecurityURL }}">Security</a></nav><h1>Security</h1>{{ if .PasswordErrorMessage }}<p>{{ .PasswordErrorMessage }}</p>{{ end }}{{ if .DeleteErrorMessage }}<p>{{ .DeleteErrorMessage }}</p>{{ end }}{{ if .EmailChangeMessage }}<p class="email-notice">{{ .EmailChangeMessage }}</p>{{ end }}{{ if .EmailErrorMessage }}<p class="email-error">{{ .EmailErrorMessage }}</p>{{ end }}<p>{{ .Email }}</p><form action="/account/security/password" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="current_password" type="password"><input name="new_password" type="password"><button type="submit">Change Password</button></form><form action="/account/security/email" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="new_email" type="email"><button type="submit">Send Confirmation Link</button></form><form action="/account/security/delete" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input name="confirm_delete"><button type="submit">Delete Account</button></form><form action="/account/logout" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><button type="submit">Log Out</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_security.html"), []byte(accountSecurity), 0644); err != nil {
		t.Fatal(err)
	}

	accountSecurityVerify := `{{ define "title" }}Verify Security Access{{ end }}{{ define "content" }}<section><h1>Verify Security Access</h1>{{ if .SuccessMessage }}<p>{{ .SuccessMessage }}</p>{{ end }}{{ if .ErrorMessage }}<p>{{ .ErrorMessage }}</p>{{ end }}<p>{{ .Email }}</p><form action="/account/security/verify" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="redirect_to" value="{{ .RedirectTo }}"><input name="password" type="password"><button type="submit">Continue</button></form><form action="/account/security/verify" method="post"><input type="hidden" name="csrf_token" value="{{ .CSRFToken }}"><input type="hidden" name="redirect_to" value="{{ .RedirectTo }}"><input type="hidden" name="action" value="email_link"><button type="submit">Email secure link</button></form></section>{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "account_security_verify.html"), []byte(accountSecurityVerify), 0644); err != nil {
		t.Fatal(err)
	}

	engine, err := themeapp.Load(dir)
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

	engine, err := themeapp.Load(dir)
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
	router.HandleFunc("GET /account/verify-email", h.AccountVerifyEmail())
	router.HandleFunc("POST /account/logout", h.Logout())
	router.HandleFunc("GET /account/orders", h.AccountOrders())
	router.HandleFunc("GET /account/orders/claim", h.AccountOrdersClaim())
	router.HandleFunc("POST /account/orders/claim", h.AccountOrdersClaim())
	router.HandleFunc("GET /account/orders/{orderId}", h.AccountOrderDetail())
	router.HandleFunc("GET /account/profile", h.AccountProfile())
	router.HandleFunc("POST /account/profile", h.AccountProfile())
	router.HandleFunc("GET /account/addresses", h.AccountAddresses())
	router.HandleFunc("POST /account/addresses", h.AccountAddressCreate())
	router.HandleFunc("POST /account/addresses/{addressId}", h.AccountAddressUpdate())
	router.HandleFunc("POST /account/addresses/{addressId}/default", h.AccountAddressSetDefault())
	router.HandleFunc("POST /account/addresses/{addressId}/delete", h.AccountAddressDelete())
	router.HandleFunc("GET /account/preferences", h.AccountPreferences())
	router.HandleFunc("POST /account/preferences", h.AccountPreferences())
	router.HandleFunc("GET /account/security", h.AccountSecurity())
	router.HandleFunc("GET /account/security/verify", h.AccountSecurityVerify())
	router.HandleFunc("POST /account/security/verify", h.AccountSecurityVerify())
	router.HandleFunc("POST /account/security/password", h.AccountPassword())
	router.HandleFunc("POST /account/security/email", h.AccountEmailChange())
	router.HandleFunc("GET /account/security/email/confirm", h.AccountEmailChangeConfirm())
	router.HandleFunc("POST /account/security/delete", h.AccountDelete())
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
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return []catalog.Category{{ID: "cat-shoes", Name: "Shoes", Slug: "shoes"}}, nil
	}}
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
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine)

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
	if !strings.Contains(body, `href="/products?category=cat-shoes`) {
		t.Fatalf("body missing interactive category facet link: %s", body)
	}
}

func TestStorefrontHandler_Products_CategoryFilterParam(t *testing.T) {
	repo := &mockStorefrontRepo{}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return []catalog.Category{{ID: "cat-shoes", Name: "Shoes", Slug: "shoes"}}, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Filters["category"] != "cat-shoes" {
			t.Fatalf("category filter = %v, want cat-shoes", query.Filters["category"])
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Runner", Slug: "runner"}},
			Total:    1,
			Facets:   map[string][]search.FacetValue{"category": []search.FacetValue{{Value: "Shoes", Count: 1}}},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products?category=cat-shoes", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Runner") {
		t.Fatalf("body missing filtered product: %s", body)
	}
	if !strings.Contains(body, `data-selected="true"`) {
		t.Fatalf("body missing selected category facet: %s", body)
	}
}

func TestStorefrontHandler_Category_InteractiveFacets(t *testing.T) {
	repo := &mockStorefrontRepo{}
	parentID := "cat-root"
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	categoryRepo := &mockStorefrontCategoryRepo{findAllFn: func(_ context.Context) ([]catalog.Category, error) {
		return []catalog.Category{
			{ID: "cat-root", Name: "Electronics", Slug: "electronics"},
			{ID: "cat-child", ParentID: &parentID, Name: "Headphones", Slug: "headphones"},
			{ID: "cat-sibling", ParentID: &parentID, Name: "Speakers", Slug: "speakers"},
		}, nil
	}}
	searchEngine := &mockSearchEngine{searchFn: func(_ context.Context, query search.SearchQuery) (search.SearchResult, error) {
		if query.Filters["category"] != "cat-child" {
			t.Fatalf("category filter = %v, want cat-child", query.Filters["category"])
		}
		return search.SearchResult{
			Products: []search.Product{{Name: "Studio Headset", Slug: "studio-headset"}},
			Total:    1,
			Facets: map[string][]search.FacetValue{
				"category": {
					{Value: "Headphones", Count: 1},
					{Value: "Speakers", Count: 2},
				},
			},
		}, nil
	}}
	h := shophttp.NewStorefrontHandler(engine, repo, categoryRepo, pdp, plp, searchEngine)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/categories/headphones", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/categories/speakers"`) {
		t.Fatalf("body missing sibling category facet link: %s", body)
	}
	if !strings.Contains(body, `href="/categories/headphones" data-selected="true"`) {
		t.Fatalf("body missing selected current category facet: %s", body)
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

type storefrontVariantRepo struct {
	variants []catalog.Variant
}

func (m *storefrontVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return m.variants, nil
}
func (m *storefrontVariantRepo) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, m.ListByProductID, productIDs, limitPerProduct)
}

func (m *storefrontVariantRepo) FindByID(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *storefrontVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *storefrontVariantRepo) FindBySKUs(_ context.Context, _ []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (m *storefrontVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *storefrontVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

type storefrontPriceRepo struct {
	price *pricing.Price
}

func (m *storefrontPriceRepo) FindByVariantCurrencyAndStore(_ context.Context, _, _, _ string) (*pricing.Price, error) {
	return m.price, nil
}
func (m *storefrontPriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, m.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

func (m *storefrontPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}
func (m *storefrontPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error { return nil }
func (m *storefrontPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

type storefrontPriceHistoryRepo struct {
	snapshot *pricing.PriceSnapshot
}

func (m *storefrontPriceHistoryRepo) Record(_ context.Context, _ *pricing.PriceSnapshot) error {
	return nil
}
func (m *storefrontPriceHistoryRepo) LowestSince(_ context.Context, _, _, _ string, _ time.Time) (*pricing.PriceSnapshot, error) {
	return m.snapshot, nil
}

func (m *storefrontPriceHistoryRepo) LowestSinceByVariants(ctx context.Context, variantIDs []string, currency, storeID string, since time.Time) (map[string]*pricing.PriceSnapshot, error) {
	return testutil.LowestSinceByVariantsFromLowest(ctx, m.LowestSince, variantIDs, currency, storeID, since)
}

func createTestThemeWithOmnibusProduct(t *testing.T) *theme.Engine {
	t.Helper()
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\nstorefront:\n  search_action: /catalog\n  cart_url: /basket\n  cart_label: Basket (2)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	layout := `<!DOCTYPE html><html><body>{{ template "content" . }}</body></html>`
	if err := os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}
	product := `{{ define "content" }}<h1>{{ .Product.Name }}</h1>{{ range .Blocks }}{{ if eq .Type "price_indication" }}<p class="price-indication">Lowest price in the last 30 days: {{ index .Data "currency" }} {{ index .Data "lowest_30d_price" }}</p>{{ end }}{{ end }}{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product.html"), []byte(product), 0644); err != nil {
		t.Fatal(err)
	}
	eng, err := themeapp.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestStorefrontHandler_Product_RendersOmnibusPriceIndication(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{ID: "p1", Name: "Widget", Slug: slug, Status: catalog.StatusActive}, nil
		},
	}
	engine := createTestThemeWithOmnibusProduct(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(composition.NewPriceIndicationStep(
		&storefrontVariantRepo{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}},
		&storefrontPriceRepo{price: &pricing.Price{VariantID: "v1", Amount: shared.MustNewMoney(3999, "EUR")}},
		&storefrontPriceHistoryRepo{snapshot: &pricing.PriceSnapshot{
			VariantID:  "v1",
			Amount:     shared.MustNewMoney(2999, "EUR"),
			RecordedAt: time.Now().UTC().AddDate(0, 0, -5),
		}},
		nil,
	))
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/widget", nil)
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Lowest price in the last 30 days") {
		t.Fatalf("body missing omnibus disclosure: %s", body)
	}
	if !strings.Contains(body, "29.99") {
		t.Fatalf("body missing lowest 30d price: %s", body)
	}
}

func createTestThemeWithWeeeProduct(t *testing.T) *theme.Engine {
	t.Helper()
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\nstorefront:\n  search_action: /catalog\n  cart_url: /basket\n  cart_label: Basket (2)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	layout := `<!DOCTYPE html><html><body>{{ template "content" . }}</body></html>`
	if err := os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}
	product := `{{ define "content" }}<h1>{{ .Product.Name }}</h1>{{ range .Blocks }}{{ if eq .Type "weee_disclosure" }}<aside class="weee-disclosure"><p>WEEE category: {{ index .Data "category_label" }}</p><p>Producer registration: {{ index .Data "producer_registration" }}</p></aside>{{ end }}{{ end }}{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product.html"), []byte(product), 0644); err != nil {
		t.Fatal(err)
	}
	eng, err := themeapp.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestStorefrontHandler_Product_RendersWeeeDisclosure(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{
				ID:     "p1",
				Name:   "Mouse",
				Slug:   slug,
				Status: catalog.StatusActive,
				Attributes: map[string]interface{}{
					legal.AttrWeeeCategory: "small_it_telecom",
				},
			}, nil
		},
	}
	engine := createTestThemeWithWeeeProduct(t)
	cfg := stubLegalConfig{
		legal.ScopedConfigKey("store-eu", legal.WeeeEnabledConfigKey):               true,
		legal.ScopedConfigKey("store-eu", legal.WeeeProducerRegistrationConfigKey): "PL-WEEE-99",
	}
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(composition.NewWeeeStep(cfg))
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithLegalConfig(cfg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/mouse", nil)
	req = req.WithContext(store.WithStore(req.Context(), &store.Store{ID: "store-eu", Name: "EU Store"}))
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Small IT and telecommunications equipment") {
		t.Fatalf("body missing WEEE category: %s", body)
	}
	if !strings.Contains(body, "PL-WEEE-99") {
		t.Fatalf("body missing producer registration: %s", body)
	}
}

func createTestThemeWithGpsrProduct(t *testing.T) *theme.Engine {
	t.Helper()
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: test\nversion: \"0.1.0\"\nstorefront:\n  search_action: /catalog\n  cart_url: /basket\n  cart_label: Basket (2)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	layout := `<!DOCTYPE html><html><body>{{ template "content" . }}</body></html>`
	if err := os.WriteFile(filepath.Join(tplDir, "layout.html"), []byte(layout), 0644); err != nil {
		t.Fatal(err)
	}
	product := `{{ define "content" }}<h1>{{ .Product.Name }}</h1>{{ range .Blocks }}{{ if eq .Type "gpsr_safety_disclosure" }}<aside class="gpsr-disclosure"><p>{{ index .Data "safety_warnings" }}</p><p>{{ index .Data "manufacturer_name" }}</p></aside>{{ end }}{{ end }}{{ end }}{{ template "layout.html" . }}`
	if err := os.WriteFile(filepath.Join(tplDir, "product.html"), []byte(product), 0644); err != nil {
		t.Fatal(err)
	}
	eng, err := themeapp.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestStorefrontHandler_Product_RendersGpsrDisclosure(t *testing.T) {
	repo := &mockStorefrontRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			return &catalog.Product{
				ID:     "p1",
				Name:   "T-Shirt",
				Slug:   slug,
				Status: catalog.StatusActive,
				Attributes: map[string]interface{}{
					legal.AttrGpsrSafetyWarnings: "Keep away from fire.",
				},
			}, nil
		},
	}
	engine := createTestThemeWithGpsrProduct(t)
	cfg := stubLegalConfig{
		legal.ScopedConfigKey("store-eu", legal.GpsrEnabledConfigKey):              true,
		legal.ScopedConfigKey("store-eu", legal.GpsrManufacturerNameConfigKey):     "Demo Apparel GmbH",
		legal.ScopedConfigKey("store-eu", legal.GpsrManufacturerContactConfigKey):  "safety@demo.example",
	}
	pdp := composition.NewPipeline[composition.ProductContext]()
	pdp.AddStep(composition.NewGpsrStep(cfg))
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, repo, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/products/tshirt", nil)
	req = req.WithContext(store.WithStore(req.Context(), &store.Store{ID: "store-eu", Name: "EU Store"}))
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Keep away from fire.") {
		t.Fatalf("body missing safety warnings: %s", body)
	}
	if !strings.Contains(body, "Demo Apparel GmbH") {
		t.Fatalf("body missing manufacturer name: %s", body)
	}
}
