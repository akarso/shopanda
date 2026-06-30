# C4 Level 3 — Component Diagram

Shows the internal components of the API Server container, organized by hexagonal architecture layers.

```mermaid
C4Component
    title Shopanda API Server — Component Diagram

    ContainerDb(postgres, "PostgreSQL", "Database")
    System_Ext(paymentGateway, "Payment Gateway", "External")
    System_Ext(meilisearch, "Meilisearch", "Search engine (optional)")
    System_Ext(redis, "Redis", "Cache/queue backend (optional)")
    System_Ext(rabbitmq, "RabbitMQ", "Queue backend (optional)")
    System_Ext(kafka, "Kafka", "Queue backend (optional)")
    System_Ext(sqs, "Amazon SQS", "Queue backend (optional)")
    System_Ext(objectStorage, "S3-compatible storage", "Object storage (optional)")

    Container_Boundary(api, "API Server") {

        Component(middleware, "Middleware Chain", "Go net/http", "Recovery → RequestID → RateLimit → Logging → Auth → Store → Language → CacheControl. Wraps all routes.")

        Boundary(interfaces, "Interfaces Layer (HTTP Handlers)") {
            Component(authHandler, "AuthHandler", "HTTP", "Register, Login, Logout, Me, PasswordReset")
            Component(productHandler, "ProductHandler", "HTTP", "List, Get products (public)")
            Component(productAdmin, "ProductAdminHandler", "HTTP", "Create, Update products (admin)")
            Component(productTranslationAdmin, "ProductTranslationAdminHandler", "HTTP", "Read/write per-language product translations for active language scope (admin)")
            Component(productPriceAdmin, "ProductPriceAdminHandler", "HTTP", "Read/write store-scoped variant prices for active store+currency scope (admin)")
            Component(pageAdmin, "PageAdminHandler", "HTTP", "Create/Update/Delete CMS pages with explicit language scope and audit (admin, content.* permissions)")
            Component(variantHandler, "VariantHandler", "HTTP", "CRUD variants")
            Component(cartHandler, "CartHandler", "HTTP", "Create, Get, AddItem, UpdateItem, RemoveItem")
            Component(checkoutHandler, "CheckoutHandler", "HTTP", "StartCheckout")
            Component(orderHandler, "OrderHandler", "HTTP", "List, Get orders")
            Component(orderAdmin, "OrderAdminHandler", "HTTP", "List, Get orders (admin)")
            Component(categoryHandler, "CategoryHandler", "HTTP", "Tree, Get, Products (public)")
            Component(searchHandler, "SearchHandler", "HTTP", "Full-text product search (public)")
            Component(mediaHandler, "MediaHandler", "HTTP", "Upload media files (admin)")
            Component(schemaHandler, "SchemaHandler", "HTTP", "Expose admin form and grid schemas (admin)")
            Component(configAdmin, "ConfigAdminHandler", "HTTP", "Grouped store settings + plugin config (admin)")
            Component(couponAdmin, "CouponAdminHandler", "HTTP", "Coupons CRUD (admin)")
            Component(promotionAdmin, "PromotionAdminHandler", "HTTP", "Promotions CRUD (admin)")
            Component(attributeAdmin, "AttributeAdminHandler", "HTTP", "Attributes and groups (admin)")
            Component(inventoryAdmin, "InventoryAdminHandler", "HTTP", "Stock list and adjust (admin)")
            Component(customerAdmin, "CustomerAdminHandler", "HTTP", "Customer list, detail, delete (admin)")
            Component(auditLogAdmin, "AuditLogAdminHandler", "HTTP", "Audit log list + CSV/JSON export (admin, audit.read)")
            Component(accountHandler, "AccountHandler", "HTTP", "Profile, consent, GDPR export/delete (customer)")
            Component(shippingHandler, "ShippingRatesHandler", "HTTP", "List shipping rates")
            Component(webhookHandler, "PaymentWebhookHandler", "HTTP", "Handle payment callbacks (public)")
            Component(storefrontHandler, "StorefrontHandler", "HTTP", "SSR storefront: catalog/PDP/PLP, cart, checkout (prefilled from default saved address), profile-side account pages incl. saved addresses + marketing preferences, and step-up-gated account email change with re-verification (optional, gated by frontend.enabled)")
            Component(storeAdmin, "StoreAdminHandler", "HTTP", "List, Create, Update stores (admin)")
        }

        Boundary(application, "Application Layer (Use Cases)") {
            Component(authService, "AuthService", "Go", "Register, Login, Logout, JWT validation, password reset")
            Component(cartService, "CartService", "Go", "Cart management with pricing integration")
            Component(checkoutWorkflow, "CheckoutWorkflow", "Go", "6-step ordered flow: validate → price → reserve → order → ship → pay")
            Component(pricingPipeline, "PricingPipeline", "Go", "Step-based price calculation: BasePriceStep → plugin steps → FinalizeStep")
            Component(compositionPipeline, "CompositionPipeline", "Go, Generics", "PDP and PLP response enrichment via plugin steps")
            Component(importerService, "ProductImporter", "Go", "Bulk CSV product import with attribute mapping and validation")
            Component(exporterService, "ProductExporter", "Go", "Paginated product/variant export to CSV with dynamic attribute columns")
            Component(stockImporter, "StockImporter", "Go", "CSV stock quantity import: SKU lookup, validation, bulk SetStock")
            Component(stockExporter, "StockExporter", "Go", "Paginated stock export to CSV with SKU resolution")
            Component(customerImporter, "CustomerImporter", "Go", "CSV customer import: email-keyed creation, role/status validation, password hashing")
            Component(customerExporter, "CustomerExporter", "Go", "Paginated customer export to CSV (email, name, role, status; no password hash)")
            Component(attrImporter, "AttributeImporter", "Go", "CSV attribute & group import: validates types, builds registry, persists to config store")
            Component(attrExporter, "AttributeExporter", "Go", "Exports attribute & group definitions from config store to CSV")
            Component(catImporter, "CategoryImporter", "Go", "CSV category import: topological sort, parent resolution, upsert")
            Component(catExporter, "CategoryExporter", "Go", "Exports category tree to CSV in parent-before-child order")
            Component(priceImporter, "PriceImporter", "Go", "CSV price import: SKU lookup, currency validation, upsert per variant+currency")
            Component(priceExporter, "PriceExporter", "Go", "Paginated price export to CSV with SKU resolution")
            Component(notifService, "NotificationService", "Go", "Listens to order.paid, renders email template, enqueues email.send job")
            Component(auditor, "Auditor", "Go", "Structured admin audit logging with best-effort DB persistence")
            Component(mediaService, "MediaService", "Go", "Upload files: validate type, save to storage, persist asset record")
            Component(cacheCleanupHandler, "CacheCleanupHandler", "Go", "Handles cache.cleanup jobs: removes expired cache entries")
            Component(auditRetentionHandler, "AuditRetentionHandler", "Go", "Handles audit.retention jobs: prunes admin_audit_log by config")
            Component(webhookDispatcher, "WebhookDispatcher", "Go", "Enqueues outbound webhook deliveries on domain events")
            Component(webhookDeliverHandler, "WebhookDeliverHandler", "Go", "Handles webhook.deliver jobs: POST signed payloads")
            Component(productSchemaRegistration, "ProductSchemaRegistration", "Go", "Registers product form and grid schemas with admin registry")
        }

        Boundary(infrastructure, "Infrastructure Layer (Adapters)") {
            Component(postgresRepos, "PostgreSQL Repositories", "Go, lib/pq", "Catalog, cart, order, customer, payment, shipping, CMS, config, and related repos")
            Component(auditLogRepo, "AuditLogRepo", "Go, lib/pq", "Insert, list, export, and retention delete for admin_audit_log")
            Component(webhookEndpointRepo, "WebhookEndpointRepo", "Go, lib/pq", "Merchant outbound webhook endpoint CRUD")
            Component(postgresSearch, "PostgresSearchEngine", "Go, tsvector", "Full-text search via PostgreSQL tsvector, filters, facets")
            Component(postgresJobQueue, "PostgresJobQueue", "Go, lib/pq", "Job queue with FOR UPDATE SKIP LOCKED dequeue, retry logic")
            Component(manualPay, "ManualPayProvider", "Go", "Offline payment processing")
            Component(flatRate, "FlatRateShipProvider", "Go", "Fixed-cost shipping calculation")
            Component(cronScheduler, "CronScheduler", "Go", "In-process cron scheduler: implements Scheduler port, fires registered tasks on schedule, enqueues jobs into Queue")
            Component(smtpMailer, "SMTPMailer", "Go, net/smtp", "Sends email via SMTP: implements Mailer port")
            Component(localFSStorage, "LocalStorage", "Go, os", "Saves/deletes files on local disk: implements Storage port")
            Component(pgCacheStore, "PostgresCacheStore", "Go, UNLOGGED table", "Key-value cache with TTL: implements Cache port")
            Component(pgConfigRepo, "PostgresConfigRepo", "Go, lib/pq", "DB-backed config storage: implements config.Repository port")
        }

        Boundary(domain, "Domain Layer") {
            Component(jobWorker, "JobWorker", "Go", "Domain-layer worker: polls Queue port, dispatches jobs to registered handlers")
            Component(adminRegistry, "AdminSchemaRegistry", "Go", "In-memory registry of Form and Grid schemas; plugins append fields, columns, actions")
            Component(attributeRegistry, "AttributeRegistry", "Go", "In-memory registry of Attribute and AttributeGroup; typed validation of product attribute values")
            Component(themeEngine, "ThemeEngine", "Go, html/template", "Loads theme templates with layout support; renders pages via Render(name, data)")
            Component(taxDomain, "TaxDomain", "Go", "TaxClass, TaxRate, TaxMode, Calculate — country-based VAT rate lookup and tax amount computation")
        }

        Boundary(platform, "Platform Layer (Cross-Cutting)") {
            Component(eventBus, "EventBus", "Go", "Pub/sub for domain events (sync + async)")
            Component(pluginRegistry, "PluginRegistry", "Go", "Plugin lifecycle: register → init → collect steps and providers")
            Component(jwtPkg, "JWT", "Go, crypto", "Token issuing and verification")
            Component(configPkg, "Config", "Go, yaml.v3", "YAML configuration loading")
            Component(loggerPkg, "Logger", "Go", "Structured logging (info, error, metadata)")
            Component(seedRegistry, "SeedRegistry", "Go", "Ordered seeder framework: register, run sequentially, idempotent")
            Component(adminSeeder, "AdminSeeder", "Go", "Seeds default admin user (admin@example.com)")
            Component(configSeeder, "ConfigSeeder", "Go", "Seeds store config (default currency EUR)")
            Component(catalogSeeder, "CatalogSeeder", "Go", "Seeds categories, products, variants, prices, stock")
        }

        Boundary(corePlugins, "Core Plugins (plugins/core/, config-gated)") {
            Component(corePostgresPlugins, "Postgres Core Plugins", "Go", "Default search, cache, queue adapters when driver=postgres")
            Component(coreMeilisearch, "Meilisearch Core Plugin", "Go", "Registers Meilisearch engine when search.engine=meilisearch")
            Component(coreRedisCache, "Redis Cache Core Plugin", "Go", "Registers Redis cache when cache.driver=redis")
            Component(coreRedisQueue, "Redis Queue Core Plugin", "Go", "Registers Redis job queue when queue.driver=redis")
            Component(coreRabbitMQ, "RabbitMQ Core Plugin", "Go", "Registers AMQP job queue when queue.driver=rabbitmq")
            Component(coreKafkaQueue, "Kafka Queue Core Plugin", "Go", "Registers Kafka job queue when queue.driver=kafka")
            Component(coreSQSQueue, "SQS Queue Core Plugin", "Go", "Registers SQS job queue when queue.driver=sqs")
            Component(coreStripe, "Stripe Core Plugin", "Go", "Registers Stripe payment provider when payment.stripe.enabled")
            Component(coreS3Storage, "S3 Storage Core Plugin", "Go", "Registers S3 media storage when storage.driver=s3")
        }

        Boundary(externalPlugins, "External Plugins (compile-time register)") {
            Component(examplePlugin, "Example Plugin", "Go", "Reference: pricing step, order.created listener, example.reports.read permission (plugins.example.enabled)")
        }

        Boundary(b2bModule, "B2B Module (commercial, plugins/b2b/)") {
            Component(b2bPlugin, "B2B Plugin", "Go", "License-gated: customer groups, quotes, PO (planned). Stub validates plugins.b2b.license_key.")
        }
    }

    Rel(middleware, authHandler, "Routes requests")
    Rel(middleware, productHandler, "Routes requests")
    Rel(middleware, cartHandler, "Routes requests")
    Rel(middleware, checkoutHandler, "Routes requests")
    Rel(middleware, categoryHandler, "Routes requests")
    Rel(middleware, searchHandler, "Routes requests")
    Rel(middleware, mediaHandler, "Routes requests")
    Rel(middleware, schemaHandler, "Routes requests")
    Rel(middleware, webhookHandler, "Routes requests")
    Rel(middleware, storefrontHandler, "Routes requests (when frontend.enabled)")
    Rel(middleware, storeAdmin, "Routes requests")
    Rel(middleware, postgresRepos, "Store resolution (StoreMiddleware)")

    Rel(authHandler, authService, "Delegates auth logic")
    Rel(cartHandler, cartService, "Delegates cart logic")
    Rel(checkoutHandler, checkoutWorkflow, "Delegates checkout flow")
    Rel(productHandler, compositionPipeline, "Enriches product responses")
    Rel(storefrontHandler, compositionPipeline, "Runs PDP pipeline")
    Rel(storefrontHandler, postgresRepos, "Product lookup by slug")
    Rel(storefrontHandler, themeEngine, "Renders product page template")

    Rel(cartService, pricingPipeline, "Prices cart items")
    Rel(checkoutWorkflow, pricingPipeline, "Recalculates pricing")
    Rel(checkoutWorkflow, manualPay, "Initiates payment")
    Rel(checkoutWorkflow, flatRate, "Selects shipping")
    Rel(checkoutWorkflow, eventBus, "Publishes checkout events")

    Rel(eventBus, notifService, "order.paid event")
    Rel(notifService, postgresRepos, "Looks up order + customer")
    Rel(notifService, postgresJobQueue, "Enqueues email.send job")
    Rel(jobWorker, smtpMailer, "EmailSendHandler sends via Mailer")
    Rel(jobWorker, cacheCleanupHandler, "Dispatches cache.cleanup jobs")
    Rel(cacheCleanupHandler, pgCacheStore, "Calls DeleteExpired")
    Rel(jobWorker, auditRetentionHandler, "Dispatches audit.retention jobs")
    Rel(auditRetentionHandler, auditLogRepo, "DeleteBefore by retention config")
    Rel(eventBus, webhookDispatcher, "order/payment events")
    Rel(webhookDispatcher, webhookEndpointRepo, "Lists active endpoints for event")
    Rel(webhookDispatcher, postgresJobQueue, "Enqueues webhook.deliver jobs")
    Rel(jobWorker, webhookDeliverHandler, "Dispatches webhook.deliver jobs")
    Rel(webhookDeliverHandler, webhookEndpointRepo, "Load endpoint secret/url")

    Rel(authService, postgresRepos, "Customer + token queries")
    Rel(cartService, postgresRepos, "Cart persistence")
    Rel(checkoutWorkflow, postgresRepos, "Order, inventory, payment, shipping persistence")
    Rel(productHandler, postgresRepos, "Product queries")
    Rel(pluginRegistry, adminRegistry, "Plugins register permissions and schemas")
    Rel(importerService, attributeRegistry, "Validates attribute values")
    Rel(exporterService, postgresRepos, "Reads products and variants")
    Rel(stockImporter, postgresRepos, "Looks up variants by SKU, writes stock entries")
    Rel(stockExporter, postgresRepos, "Lists stock entries, looks up variants by ID")
    Rel(customerImporter, postgresRepos, "Creates customer records")
    Rel(customerExporter, postgresRepos, "Lists customers with pagination")
    Rel(attrImporter, pgConfigRepo, "Persists attribute definitions")
    Rel(attrExporter, pgConfigRepo, "Reads attribute definitions")
    Rel(catImporter, postgresRepos, "Reads and writes categories")
    Rel(catExporter, postgresRepos, "Reads categories")
    Rel(priceImporter, postgresRepos, "Looks up variants by SKU, upserts prices")
    Rel(priceExporter, postgresRepos, "Lists prices, resolves variant SKUs")
    Rel(seedRegistry, postgresRepos, "Seeders access repos via Deps.DB")
    Rel(adminSeeder, seedRegistry, "Registered as seeder")
    Rel(configSeeder, seedRegistry, "Registered as seeder")
    Rel(catalogSeeder, seedRegistry, "Registered as seeder")
    Rel(productSchemaRegistration, adminRegistry, "Registers product form + grid")
    Rel(categoryHandler, postgresRepos, "Category + product queries")
    Rel(searchHandler, postgresSearch, "Delegates search queries")
    Rel(mediaHandler, mediaService, "Delegates upload logic")
    Rel(schemaHandler, adminRegistry, "Reads form and grid schemas")
    Rel(mediaService, localFSStorage, "Saves files")
    Rel(mediaService, postgresRepos, "Persists asset records")
    Rel(taxDomain, postgresRepos, "Tax rate queries")
    Rel(storeAdmin, postgresRepos, "Store CRUD")
    Rel(storeAdmin, eventBus, "Publishes store.created, store.updated")

    Rel(postgresRepos, postgres, "SQL queries", "lib/pq")
    Rel(postgresSearch, postgres, "Full-text search queries", "lib/pq")
    Rel(postgresJobQueue, postgres, "Job queue queries", "lib/pq")
    Rel(pgCacheStore, postgres, "Key-value cache queries", "lib/pq")
    Rel(pgConfigRepo, postgres, "Config key-value queries", "lib/pq")
    Rel(jobWorker, postgresJobQueue, "Polls and claims jobs")
    Rel(cronScheduler, postgresJobQueue, "Enqueues scheduled jobs")
    Rel(webhookHandler, paymentGateway, "Receives callbacks")
    Rel(pluginRegistry, pricingPipeline, "Provides pricing steps via pluginApp")
    Rel(pluginRegistry, checkoutWorkflow, "Provides checkout steps via pluginApp")
    Rel(pluginRegistry, compositionPipeline, "Provides composition steps via pluginApp")
    Rel(pluginRegistry, eventBus, "Provides event handlers via pluginApp")

    Rel(configPkg, pluginRegistry, "Driver switches select active core plugins")
    Rel(pluginRegistry, corePostgresPlugins, "Registers when postgres drivers active")
    Rel(pluginRegistry, coreMeilisearch, "Registers when search.engine=meilisearch")
    Rel(pluginRegistry, coreRedisCache, "Registers when cache.driver=redis")
    Rel(pluginRegistry, coreRedisQueue, "Registers when queue.driver=redis")
    Rel(pluginRegistry, coreRabbitMQ, "Registers when queue.driver=rabbitmq")
    Rel(pluginRegistry, coreKafkaQueue, "Registers when queue.driver=kafka")
    Rel(pluginRegistry, coreSQSQueue, "Registers when queue.driver=sqs")
    Rel(pluginRegistry, coreStripe, "Registers when payment.stripe.enabled")
    Rel(pluginRegistry, coreS3Storage, "Registers when storage.driver=s3")
    Rel(pluginRegistry, examplePlugin, "Registers when plugins.example.enabled")
    Rel(pluginRegistry, b2bPlugin, "Registers when plugins.b2b.enabled + valid license")

    Rel(corePostgresPlugins, postgresSearch, "Provides default search engine")
    Rel(corePostgresPlugins, pgCacheStore, "Provides default cache store")
    Rel(corePostgresPlugins, postgresJobQueue, "Provides default job queue")
    Rel(coreMeilisearch, meilisearch, "Indexes and queries products", "HTTP")
    Rel(coreRedisCache, redis, "Key-value cache with TTL", "Redis protocol")
    Rel(coreRedisQueue, redis, "Job enqueue/dequeue", "Redis protocol")
    Rel(coreRabbitMQ, rabbitmq, "AMQP job dispatch", "AMQP")
    Rel(coreKafkaQueue, kafka, "Topic-based job dispatch", "Kafka protocol")
    Rel(coreSQSQueue, sqs, "Queue-based job dispatch", "HTTPS")
    Rel(coreStripe, paymentGateway, "Payment initiation and refunds", "HTTPS")
    Rel(coreS3Storage, objectStorage, "Media object read/write", "HTTPS")
    Rel(examplePlugin, pricingPipeline, "Example fee pricing step via pluginApp")
    Rel(examplePlugin, eventBus, "order.created async listener via pluginApp")
```

> **Wiring note (post PR-410–434):** `cmd/api/register_plugins.go` calls `core.Register()` for config-gated core plugins and optionally registers external plugins (e.g. `plugins/example`). A shared `Auditor` with `AuditLogRepo` is wired to admin mutation handlers. `PluginRegistry` calls `plugin.Init(pluginApp)` for each registered plugin. Core plugins set infrastructure providers on `pluginApp`; external plugins register pipeline steps, event handlers, permissions, and optional `RegisterConfig` settings. `main.go` resolves providers and extracts steps into pipelines, workflows, and the event bus — explicit hexagonal wiring, not runtime discovery.
>
> **Phase 5 (planned):** returns/RMA, customer groups, advanced promotions, EU compliance fields (WEEE/EPR/GPSR), admin MFA — see [Phase 5 Roadmap](../phase-5-maturity/ROADMAP.md). **[b2b]** features ship in `plugins/b2b/` under commercial license — see [Commercial Licensing](../COMMERCIAL.md).
>
> **Deferred:** dynamic `.so` loading, plugin marketplace.
