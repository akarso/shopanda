# C4 Level 2 — Container Diagram

Shows the major containers (deployable units) within the Shopanda system.

```mermaid
C4Container
    title Shopanda — Container Diagram

    Person(customer, "Customer", "Browses catalog, manages cart, places orders")
    Person(admin, "Admin", "Manages products, categories, collections, orders")
    System_Ext(paymentGateway, "Payment Gateway", "External payment processor")

    System_Boundary(shopanda, "Shopanda System") {
        Container(apiServer, "API Server", "Go, net/http", "HTTP server: REST API, admin SPA, optional SSR storefront. Hexagonal layers: domain, application, infrastructure, interfaces.")
        Container(worker, "Worker", "Go, same binary", "Background job processor (email, cache cleanup, async tasks). Embedded in `app dev`/`serve`; separate service in production.")
        Container(scheduler, "Scheduler", "Go, same binary", "Cron scheduler enqueueing recurring jobs. Embedded in `app dev`; separate service in production.")
        Container(pluginSystem, "Plugin System", "Go interfaces", "Three-tier extensions: core plugins (config-gated), external plugins (compile-time). Events, pipelines, workflows.")
        Container(eventBus, "Event Bus", "Go, in-process", "Publish/subscribe for domain events. Sync and async handlers.")
        ContainerDb(postgres, "PostgreSQL", "PostgreSQL", "Products, orders, customers, jobs, cache, config, audit log, and related commerce data (~47 migrations).")
    }

    Rel(customer, apiServer, "Storefront and REST API", "HTTPS")
    Rel(admin, apiServer, "Admin UI and REST API (authenticated)", "HTTPS")
    Rel(paymentGateway, apiServer, "Webhook callbacks", "HTTPS POST")
    Rel(apiServer, postgres, "Reads/writes data", "SQL / lib/pq")
    Rel(worker, postgres, "Claims and completes jobs", "SQL / lib/pq")
    Rel(scheduler, postgres, "Enqueues scheduled jobs", "SQL / lib/pq")
    Rel(apiServer, eventBus, "Publishes domain events", "In-process")
    Rel(eventBus, pluginSystem, "Delivers events to plugin handlers", "In-process")
    Rel(pluginSystem, apiServer, "Registers pricing, checkout, composition steps", "In-process")

    UpdateRelStyle(customer, apiServer, $offsetY="-40")
    UpdateRelStyle(admin, apiServer, $offsetY="40")
```

> **Runtime layout:** Development uses `app dev` (single process with embedded worker + scheduler). Production runs `serve`, `worker`, and `scheduler` as separate services from the same image. See [Runtime Modes](../phase-4-refactoring/specs/RUNTIME_MODES.md).
