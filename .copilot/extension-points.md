# Shopanda Extension Points & Cross-PR Dependencies

## Extension Points (how plugins connect)

### 1. Composition Pipeline (PR-012)
- Generic Pipeline[T] with Step[T] interface: Name() string, Apply(ctx *T) error
- Two pipelines: PDP (ProductContext) and PLP (ListingContext)
- ProductContext: Product, Currency, Country, Blocks, Meta
- ListingContext: Products, Filters, SortOptions, Blocks, Currency, Country, Meta
- Block: Type string + Data map (UI-agnostic)
- Plugin registration: app.RegisterCompositionStep("pdp", step) or ("plp", step)
- main.go extracts: pluginApp.CompositionSteps("pdp") → type-asserts → pdp.AddStep()
- USED BY: ProductHandler.Get (PDP), ProductHandler.List (PLP)

### 2. Pricing Pipeline (PR-018–020)
- PricingStep interface: Name() string, Apply(ctx context.Context, pctx *PricingContext) error
- PricingContext: Currency, Items[]PricingItem, Subtotal/DiscountsTotal/TaxTotal/FeesTotal/GrandTotal (Money), Adjustments, Meta
- PricingItem: VariantID, Quantity, UnitPrice, Total (Money), Adjustments
- Adjustment: Type (discount/tax/fee), Code, Description, Amount (Money), Included, Meta
- Pipeline execution: BasePriceStep → [plugin steps] → FinalizeStep
- FinalizeStep: sums items→Subtotal, aggregates adjustments, GrandTotal = Subtotal - Discounts + Tax + Fees
- Plugin registration: app.RegisterPricingStep(step)
- main.go extracts: pluginApp.PricingSteps() → type-asserts → append between base and finalize
- USED BY: CartService (prices items), CheckoutWorkflow (recalculate_pricing step)

### 3. Checkout Workflow (PR-035–038)
- Step interface: Name() string, Execute(ctx *Context) error
- Context: CartID, Cart, CustomerID, Currency, Order, Meta, Trace
- Built-in steps (in order):
  1. validate_cart — needs: VariantRepository
  2. recalculate_pricing — needs: pricing.Pipeline
  3. reserve_inventory — needs: ReservationRepository (default 15 min TTL)
  4. create_order — needs: OrderRepository, VariantRepository
  5. select_shipping — needs: shipping.Provider, ShipmentRepository
  6. initiate_payment — needs: payment.Provider, PaymentRepository
- Plugin steps appended AFTER core steps
- Plugin registration: app.RegisterCheckoutStep(step)
- main.go extracts: pluginApp.CheckoutSteps() → type-asserts → append after core
- Workflow publishes events: checkout.step.started, checkout.step.completed, checkout.failed, checkout.completed

### 4. Event Bus (PR-029–030)
- Event: ID, Name, Version, Timestamp, Source, Data, Meta
- On(name, handler) for sync, OnAsync(name, handler) for async
- Publish: sync handlers first (fail-fast), then async in goroutines
- Known events: catalog.product.created/updated, catalog.variant.created, cart.created/item.added/item.removed, order.created/confirmed, inventory.reserved/released, payment.initiated/completed/failed, shipping.selected/shipped, customer.registered/password_reset_requested/password_reset_confirmed, checkout.step.started/step.completed/failed/completed

### 5. Plugin System (PR-048–049)
- Plugin interface: Name() string, Init(app *App) error
- App struct: holds Logger, Bus, Config + registered steps ([]any)
- Registration: RegisterPricingStep(any), RegisterCheckoutStep(any), RegisterCompositionStep(pipeline string, any)
- Getters: PricingSteps(), CheckoutSteps(), CompositionSteps(pipeline) — return defensive copies
- Registry: Register(plugin), InitAll(app) → InitSummary{Registered, Initialized, Failed}
- Lifecycle states: loaded → active | failed
- safeInit() defers panic recovery; failed plugins don't crash system

## Cross-PR Dependency Map

### Critical Dependencies (future PRs MUST know about)
- PR-012 (composition) → used by PR-013 (product handlers), any future PDP/PLP enrichment
- PR-018-020 (pricing) → used by PR-027 (cart service), PR-036 (checkout recalculate), any future pricing plugins
- PR-035-038 (checkout) → orchestrates PR-023-024 (inventory), PR-034 (orders), PR-042-043 (payments), PR-045-046 (shipping)
- PR-048-049 (plugin system) → used by ALL future plugins; registration happens via app.RegisterXxxStep()
- PR-029-030 (events) → used by checkout, auth, cart; any future event listeners
- PR-021-022 (auth) → RequireAuth/RequireRole middleware used by all protected routes
- PR-053 (search interface) → will be implemented by PR-054 (postgres search), consumed by PR-055 (search endpoint)

### Interface Contracts That Affect Multiple Files
- Adding a method to any Repository interface requires updating ALL mock implementations in test files
- Known mocks to update when catalog interfaces change:
  - internal/interfaces/http/product_test.go (mockProductRepo)
  - internal/interfaces/http/product_admin_test.go (mockAdminProductRepo)
  - internal/interfaces/http/variant_test.go (mockVariantRepo + mockAdminVariantProductRepo)
  - internal/application/importer/products_test.go (mockImporterProductRepo)

### main.go Wiring Order (runServe function)
1. Config → Logger → DB connection
2. 13 Repositories (postgres.NewXxxRepo)
3. 2 Providers (manualpay, flatrate at $5.00 USD)
4. Event Bus
5. Dev auth handler (bus.On password reset requested)
6. Plugin Registry → Register plugins → InitAll(pluginApp)
7. Composition Pipelines (PDP + PLP) ← extract plugin steps
8. Pricing Pipeline (BasePriceStep + plugin steps + FinalizeStep)
9. Checkout workflow steps → Workflow ← extract plugin steps
10. Application services (cart, checkout, auth)
11. JWT setup (issuer + validating parser)
12. HTTP Handlers (10+)
13. Router + middleware chain + routes (33 routes)
14. HTTP server ListenAndServe

### Migration Numbering
- Latest: 015 (collections). Next available: 016.
