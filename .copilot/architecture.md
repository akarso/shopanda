# Shopanda Architecture Reference

## Module & Stack
- `github.com/akarso/shopanda`, Go 1.25.6, PostgreSQL
- Single binary: `cmd/api/main.go` with subcommands: serve, migrate, import:products
- Dependencies: lib/pq, yaml.v3, golang.org/x/crypto (bcrypt, JWT)

## Hexagonal Layers
```
interfaces (HTTP) → application (use cases) → domain (entities, ports)
                           ↓
                    infrastructure (adapters)
                           ↓
                      platform (cross-cutting)
```

## Domain Packages (internal/domain/)
- `catalog` — Product, Variant, Category, Collection + repos
- `cart` — Cart, Item + CartRepository
- `order` — Order, OrderItem + OrderRepository
- `inventory` — Stock, Reservation + repos
- `pricing` — PricingContext, PricingItem, Adjustment, PricingStep, FinalizeStep, Pipeline
- `payment` — Payment + PaymentRepository
- `shipping` — Shipment + ShipmentRepository
- `customer` — Customer, PasswordResetToken + repos
- `identity` — Identity (role: guest/customer/admin)
- `shared` — Money (int64 amount + ISO 4217 currency)
- `search` — SearchQuery, SearchResult, FacetValue, Product (search-local), SearchEngine interface

## Application Packages (internal/application/)
- `auth` — Service (register, login, logout, password reset), ValidatingTokenParser
- `cart` — Service (CRUD + pricing integration)
- `checkout` — Workflow (6 ordered steps), Step interface, Context
- `composition` — Generic Pipeline[T], Step[T] interface, ProductContext (PDP), ListingContext (PLP), Block
- `pricing` — BasePriceStep (loads base price from repo)
- `importer` — ProductImporter (CSV)

## Infrastructure Packages (internal/infrastructure/)
- `postgres` — 13 repo implementations (all support WithTx for transactions)
- `manualpay` — Manual payment provider
- `flatrate` — Flat-rate shipping provider (configurable cost)
- `devauth` — Dev-mode JWT parser

## Platform Packages (internal/platform/)
- `config` — YAML config loading
- `db` — PostgreSQL connection (db.Open)
- `logger` — Structured logging: Info(event, map), Warn(event, map), Error(event, err, map)
- `id` — UUID v4 generation (id.New())
- `jwt` — Token issuing/verification. Create(subject, role, gen) → (token, error)
- `password` — bcrypt hashing
- `migrate` — SQL migration runner
- `event` — Event bus (pub/sub): On (sync), OnAsync (async), Publish
- `plugin` — Plugin interface + Registry + App (registration context)
- `apperror` — Validation(msg)→422, NotFound(msg)→404, Conflict(msg)→409, Internal(msg)→500, Wrap(code,msg,err)
- `requestctx` — Request context utilities, correlation IDs
- `auth` — Auth middleware, RequireAuth, RequireRole

## Key Interfaces (Ports)
- ProductRepository: FindByID, FindBySlug, List, FindByCategoryID, Create, Update, WithTx
- VariantRepository: FindByID, FindByProductID, List, Create, Update, WithTx
- CartRepository: FindByID, FindActiveByCustomerID, Save, Delete (NO WithTx)
- OrderRepository: FindByID, FindByCustomerID, List, Save, UpdateStatus (NO WithTx)
- CategoryRepository: FindByID, FindBySlug, FindAll, Create, Update
- CollectionRepository: FindByID, FindBySlug, List, Create, Update, AddProduct, RemoveProduct, ListProductIDs
- PriceRepository: FindByVariantAndCurrency, Create, Update, WithTx
- StockRepository: FindByVariantID, Update, WithTx
- ReservationRepository: Create, FindByVariantID, FindByOrderID, WithTx
- CustomerRepository: FindByID, FindByEmail, Create, Update
- PaymentRepository: FindByID, FindByOrderID, Create, UpdateStatus, WithTx
- ShippingRepository: FindByOrderID, Create, Update, WithTx
- SearchEngine: Name, IndexProduct, RemoveProduct, Search

## PK Type Conventions in Migrations
- UUID PKs: products, variants, prices, stock, reservations, carts, cart_items, customers, password_reset_tokens, orders, order_items
- TEXT PKs: payments, shipments, categories, collections, collection_products (collection_id)
- collection_products.product_id is UUID (matches products.id)
- product_categories.product_id is TEXT (MISMATCH with products.id UUID — pre-existing issue from PR-050)
