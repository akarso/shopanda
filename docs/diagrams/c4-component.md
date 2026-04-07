# C4 Level 3 — Component Diagram

Shows the internal components of the API Server container, organized by hexagonal architecture layers.

```mermaid
C4Component
    title Shopanda API Server — Component Diagram

    ContainerDb(postgres, "PostgreSQL", "Database")
    System_Ext(paymentGateway, "Payment Gateway", "External")

    Container_Boundary(api, "API Server") {

        Component(middleware, "Middleware Chain", "Go net/http", "Recovery → RequestID → Logging → Auth. Wraps all routes.")

        Boundary(interfaces, "Interfaces Layer (HTTP Handlers)") {
            Component(authHandler, "AuthHandler", "HTTP", "Register, Login, Logout, Me, PasswordReset")
            Component(productHandler, "ProductHandler", "HTTP", "List, Get products (public)")
            Component(productAdmin, "ProductAdminHandler", "HTTP", "Create, Update products (admin)")
            Component(variantHandler, "VariantHandler", "HTTP", "CRUD variants")
            Component(cartHandler, "CartHandler", "HTTP", "Create, Get, AddItem, UpdateItem, RemoveItem")
            Component(checkoutHandler, "CheckoutHandler", "HTTP", "StartCheckout")
            Component(orderHandler, "OrderHandler", "HTTP", "List, Get orders")
            Component(orderAdmin, "OrderAdminHandler", "HTTP", "List, Get orders (admin)")
            Component(categoryHandler, "CategoryHandler", "HTTP", "Tree, Get, Products (public)")
            Component(searchHandler, "SearchHandler", "HTTP", "Full-text product search (public)")
            Component(shippingHandler, "ShippingRatesHandler", "HTTP", "List shipping rates")
            Component(webhookHandler, "PaymentWebhookHandler", "HTTP", "Handle payment callbacks (public)")
        }

        Boundary(application, "Application Layer (Use Cases)") {
            Component(authService, "AuthService", "Go", "Register, Login, Logout, JWT validation, password reset")
            Component(cartService, "CartService", "Go", "Cart management with pricing integration")
            Component(checkoutWorkflow, "CheckoutWorkflow", "Go", "6-step ordered flow: validate → price → reserve → order → ship → pay")
            Component(pricingPipeline, "PricingPipeline", "Go", "Step-based price calculation: BasePriceStep → plugin steps → FinalizeStep")
            Component(compositionPipeline, "CompositionPipeline", "Go, Generics", "PDP and PLP response enrichment via plugin steps")
            Component(importerService, "ProductImporter", "Go", "Bulk CSV product import")
        }

        Boundary(infrastructure, "Infrastructure Layer (Adapters)") {
            Component(postgresRepos, "PostgreSQL Repositories", "Go, lib/pq", "13 repo implementations: Product, Variant, Price, Cart, Order, Customer, Stock, Reservation, Payment, Shipping, Category, Collection, ResetToken")
            Component(postgresSearch, "PostgresSearchEngine", "Go, tsvector", "Full-text search via PostgreSQL tsvector, filters, facets")
            Component(postgresJobQueue, "PostgresJobQueue", "Go, lib/pq", "Job queue with FOR UPDATE SKIP LOCKED dequeue, retry logic")
            Component(manualPay, "ManualPayProvider", "Go", "Offline payment processing")
            Component(flatRate, "FlatRateShipProvider", "Go", "Fixed-cost shipping calculation")
        }

        Boundary(platform, "Platform Layer (Cross-Cutting)") {
            Component(eventBus, "EventBus", "Go", "Pub/sub for domain events (sync + async)")
            Component(pluginRegistry, "PluginRegistry", "Go", "Plugin lifecycle: register → init → collect steps")
            Component(jobWorker, "JobWorker", "Go", "Polls queue, dispatches jobs to registered handlers")
            Component(jwtPkg, "JWT", "Go, crypto", "Token issuing and verification")
            Component(configPkg, "Config", "Go, yaml.v3", "YAML configuration loading")
            Component(loggerPkg, "Logger", "Go", "Structured logging (info, error, metadata)")
        }
    }

    Rel(middleware, authHandler, "Routes requests")
    Rel(middleware, productHandler, "Routes requests")
    Rel(middleware, cartHandler, "Routes requests")
    Rel(middleware, checkoutHandler, "Routes requests")
    Rel(middleware, categoryHandler, "Routes requests")
    Rel(middleware, searchHandler, "Routes requests")
    Rel(middleware, webhookHandler, "Routes requests")

    Rel(authHandler, authService, "Delegates auth logic")
    Rel(cartHandler, cartService, "Delegates cart logic")
    Rel(checkoutHandler, checkoutWorkflow, "Delegates checkout flow")
    Rel(productHandler, compositionPipeline, "Enriches product responses")

    Rel(cartService, pricingPipeline, "Prices cart items")
    Rel(checkoutWorkflow, pricingPipeline, "Recalculates pricing")
    Rel(checkoutWorkflow, manualPay, "Initiates payment")
    Rel(checkoutWorkflow, flatRate, "Selects shipping")
    Rel(checkoutWorkflow, eventBus, "Publishes checkout events")

    Rel(authService, postgresRepos, "Customer + token queries")
    Rel(cartService, postgresRepos, "Cart persistence")
    Rel(checkoutWorkflow, postgresRepos, "Order, inventory, payment, shipping persistence")
    Rel(productHandler, postgresRepos, "Product queries")
    Rel(categoryHandler, postgresRepos, "Category + product queries")
    Rel(searchHandler, postgresSearch, "Delegates search queries")

    Rel(postgresRepos, postgres, "SQL queries", "lib/pq")
    Rel(postgresSearch, postgres, "Full-text search queries", "lib/pq")
    Rel(postgresJobQueue, postgres, "Job queue queries", "lib/pq")
    Rel(jobWorker, postgresJobQueue, "Polls and claims jobs")
    Rel(webhookHandler, paymentGateway, "Receives callbacks")
    Rel(pluginRegistry, pricingPipeline, "Provides pricing steps via pluginApp")
    Rel(pluginRegistry, checkoutWorkflow, "Provides checkout steps via pluginApp")
    Rel(pluginRegistry, compositionPipeline, "Provides composition steps via pluginApp")
    Rel(pluginRegistry, eventBus, "Provides event handlers via pluginApp")
```

> **Wiring note:** PluginRegistry calls `plugin.Init(pluginApp)` for each plugin.
> Plugins register steps/handlers into `pluginApp` during init.
> `main.go` then extracts those steps from `pluginApp` and wires them into
> the pipelines, workflows, and event bus — hexagonal-style explicit wiring,
> not direct injection by the registry.
