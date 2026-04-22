package http_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── E2E stubs ───────────────────────────────────────────────────────────

type e2eCartRepo struct {
	carts map[string]*domainCart.Cart
}

func (r *e2eCartRepo) cloneCart(c *domainCart.Cart) *domainCart.Cart {
	clone := *c
	clone.Items = make([]domainCart.Item, len(c.Items))
	copy(clone.Items, c.Items)
	return &clone
}
func (r *e2eCartRepo) FindByID(_ context.Context, id string) (*domainCart.Cart, error) {
	c, ok := r.carts[id]
	if !ok {
		return nil, nil
	}
	return r.cloneCart(c), nil
}
func (r *e2eCartRepo) FindActiveByCustomerID(_ context.Context, cid string) (*domainCart.Cart, error) {
	for _, c := range r.carts {
		if c.CustomerID == cid && c.IsActive() {
			return r.cloneCart(c), nil
		}
	}
	return nil, nil
}
func (r *e2eCartRepo) Save(_ context.Context, c *domainCart.Cart) error {
	clone := *c
	clone.Items = make([]domainCart.Item, len(c.Items))
	copy(clone.Items, c.Items)
	r.carts[c.ID] = &clone
	return nil
}
func (r *e2eCartRepo) Delete(_ context.Context, id string) error {
	delete(r.carts, id)
	return nil
}

type e2eVariantRepo struct {
	variants map[string]*catalog.Variant
}

func (r *e2eVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return r.variants[id], nil
}
func (r *e2eVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *e2eVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *e2eVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (r *e2eVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }
func (r *e2eVariantRepo) WithTx(*sql.Tx) catalog.VariantRepository       { return r }

type e2ePriceRepo struct {
	prices map[string]*pricing.Price
}

func (r *e2ePriceRepo) FindByVariantCurrencyAndStore(_ context.Context, vid, cur, sid string) (*pricing.Price, error) {
	return r.prices[vid+":"+cur+":"+sid], nil
}
func (r *e2ePriceRepo) ListByVariantID(context.Context, string) ([]pricing.Price, error) {
	return nil, nil
}
func (r *e2ePriceRepo) List(context.Context, int, int) ([]pricing.Price, error) { return nil, nil }
func (r *e2ePriceRepo) Upsert(context.Context, *pricing.Price) error            { return nil }

type e2eReservationRepo struct{}

func (r *e2eReservationRepo) Reserve(context.Context, *inventory.Reservation) error { return nil }
func (r *e2eReservationRepo) Release(context.Context, string) error                 { return nil }
func (r *e2eReservationRepo) Confirm(context.Context, string) error                 { return nil }
func (r *e2eReservationRepo) FindByID(context.Context, string) (*inventory.Reservation, error) {
	return nil, nil
}
func (r *e2eReservationRepo) ListActiveByVariantID(context.Context, string) ([]inventory.Reservation, error) {
	return nil, nil
}
func (r *e2eReservationRepo) ReleaseExpiredBefore(context.Context, time.Time) (int, error) {
	return 0, nil
}

type e2eOrderRepo struct {
	saved *order.Order
}

func (r *e2eOrderRepo) FindByID(context.Context, string) (*order.Order, error) { return nil, nil }
func (r *e2eOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (r *e2eOrderRepo) List(context.Context, int, int) ([]order.Order, error) { return nil, nil }
func (r *e2eOrderRepo) Save(_ context.Context, o *order.Order) error {
	r.saved = o
	return nil
}
func (r *e2eOrderRepo) UpdateStatus(context.Context, *order.Order) error { return nil }

func e2eLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

// ── E2E smoke test: cart → add item → checkout → order ──────────────────

func TestE2E_CartToCheckout(t *testing.T) {
	// Wire all shared repos.
	carts := &e2eCartRepo{carts: make(map[string]*domainCart.Cart)}
	prices := &e2ePriceRepo{prices: make(map[string]*pricing.Price)}
	variants := &e2eVariantRepo{variants: make(map[string]*catalog.Variant)}
	reservations := &e2eReservationRepo{}
	orders := &e2eOrderRepo{}
	log := e2eLogger()
	bus := event.NewBus(log)

	// Seed variant + price.
	v, _ := catalog.NewVariant("var-e2e-1", "prod-e2e-1", "SKU-E2E")
	v.Name = "E2E Widget"
	variants.variants["var-e2e-1"] = &v

	p, _ := pricing.NewPrice("price-e2e-1", "var-e2e-1", "", shared.MustNewMoney(2000, "EUR"))
	prices.prices["var-e2e-1:EUR:"] = &p

	// Build services.
	pipeline := pricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		pricing.NewFinalizeStep(),
	)

	cartSvc := cartApp.NewService(carts, prices, nil, nil, pipeline, log, bus)
	cartHandler := shophttp.NewCartHandler(cartSvc)

	validateStep := checkoutApp.NewValidateCartStep(variants)
	pricingStep := checkoutApp.NewRecalculatePricingStep(pipeline)
	reserveStep := checkoutApp.NewReserveInventoryStep(reservations)
	createOrderStep := checkoutApp.NewCreateOrderStep(orders, variants)
	workflow := checkoutApp.NewWorkflow([]checkoutApp.Step{
		validateStep, pricingStep, reserveStep, createOrderStep,
	}, bus, log)

	checkoutSvc := checkoutApp.NewService(carts, workflow, log)
	checkoutHandler := shophttp.NewCheckoutHandler(checkoutSvc)

	requireAuth := shophttp.RequireAuth()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/carts", requireAuth(cartHandler.Create()))
	mux.Handle("GET /api/v1/carts/{cartId}", requireAuth(cartHandler.Get()))
	mux.Handle("POST /api/v1/carts/{cartId}/items", requireAuth(cartHandler.AddItem()))
	mux.Handle("POST /api/v1/checkout", requireAuth(checkoutHandler.StartCheckout()))

	// Step 1: Create cart.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/carts", strings.NewReader(`{"currency":"EUR"}`))
	req = testhelper.CustomerRequest(req, "cust-e2e")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create cart: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		Data struct {
			Cart struct {
				ID string `json:"id"`
			} `json:"cart"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	cartID := createResp.Data.Cart.ID
	if cartID == "" {
		t.Fatal("create cart returned empty id")
	}

	// Step 2: Add item.
	rec = httptest.NewRecorder()
	addBody := `{"variant_id":"var-e2e-1","quantity":2}`
	req = httptest.NewRequest("POST", "/api/v1/carts/"+cartID+"/items", strings.NewReader(addBody))
	req = testhelper.CustomerRequest(req, "cust-e2e")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("add item: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Step 3: Checkout.
	rec = httptest.NewRecorder()
	checkoutBody := `{"cart_id":"` + cartID + `","address":{"first_name":"Ada","last_name":"Lovelace","street":"1 Logic Lane","city":"Berlin","postcode":"10115","country":"DE"}}`
	req = httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(checkoutBody))
	req = testhelper.CustomerRequest(req, "cust-e2e")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Verify order was created.
	if orders.saved == nil {
		t.Fatal("checkout did not create an order")
	}
	if orders.saved.CustomerID != "cust-e2e" {
		t.Fatalf("order customer = %q, want cust-e2e", orders.saved.CustomerID)
	}
	if len(orders.saved.Items()) != 1 {
		t.Fatalf("order items = %d, want 1", len(orders.saved.Items()))
	}
	if orders.saved.Items()[0].Quantity != 2 {
		t.Fatalf("order item quantity = %d, want 2", orders.saved.Items()[0].Quantity)
	}
}
