# 📚 Documentation & Guides — Specification

## 1. Overview

Three documentation tracks:

* **Merchant guide** — for store operators (non-technical)
* **Deployment guide** — for DevOps / technical users
* **Developer extension guide** — for plugin authors

Design goals:

* task-oriented (how to do X)
* no assumed prior knowledge (for merchant guide)
* copy-paste commands (for deployment guide)
* real code examples (for developer guide)

---

## 2. Merchant Guide (PR-230)

---

### Location

```text
docs/guides/MERCHANT.md
```

---

### Sections

#### Getting Started
* First login to admin
* Store configuration (name, currency, address)
* Understanding the dashboard

#### Managing Products
* Creating a product
* Adding variants (sizes, colors)
* Setting prices
* Uploading images
* Organizing into categories
* Import/export via CSV

#### Managing Orders
* Viewing orders
* Updating order status (paid → shipped → delivered)
* Issuing refunds (if Stripe enabled)
* Viewing invoices

#### Store Configuration
* Email settings (SMTP)
* Shipping zones and rates
* Tax configuration
* Payment provider setup
* Multi-store setup (if applicable)

#### Day-to-Day Operations
* Processing orders workflow
* Low stock alerts
* Customer inquiries (via order data)
* Email notifications overview

---

## 3. Deployment Guide (PR-231)

---

### Location

```text
docs/guides/DEPLOYMENT.md
```

---

### Sections

#### Quick Start (Docker)
```bash
git clone https://github.com/akarso/shopanda.git
cd shopanda
cp .env.example .env
# Edit .env with your values
docker compose up -d
docker compose exec app ./app setup
```

#### Environment Variables
* Complete reference table
* Required vs optional
* Security notes (JWT secret, DB password)

#### Docker Deployment
* Building the image
* Docker Compose for production
* Volume management (media, database)
* Backup and restore

#### Cloud Platforms
* **Railway**: one-click deploy config
* **Fly.io**: `fly.toml` example
* **DigitalOcean App Platform**: spec file
* **VPS / bare metal**: systemd service file

#### Bare Metal
```bash
# Build
go build -o shopanda ./cmd/api

# Run
./shopanda migrate
./shopanda seed
./shopanda serve &
./shopanda worker &
./shopanda scheduler &
```

#### TLS / HTTPS
* Reverse proxy with Caddy (auto TLS)
* Nginx + Let's Encrypt example

#### Database
* PostgreSQL setup
* Connection pooling (PgBouncer)
* Backup script example

#### Monitoring
* Health endpoint: `GET /api/v1/health`
* Structured JSON logs
* Log aggregation suggestions

---

## 4. Developer Extension Guide (PR-232)

---

### Location

```text
docs/guides/DEVELOPER.md
```

---

### Sections

#### Architecture Overview
* Hexagonal architecture diagram
* Layer responsibilities
* Dependency direction

#### Creating a Plugin
```go
type MyPlugin struct{}

func (p *MyPlugin) Name() string    { return "my-plugin" }
func (p *MyPlugin) Version() string { return "1.0.0" }
func (p *MyPlugin) Init(app *Application) error {
    // Register steps, handlers, event listeners
    return nil
}
```

#### Adding a Payment Provider
* Implementing `PaymentProvider` interface
* Webhook handler registration
* Testing with mock webhooks
* Configuration integration

#### Adding a Shipping Provider
* Implementing `ShippingProvider` interface
* Rate calculation
* Zone integration

#### Adding a Storage Backend
* Implementing `Storage` interface
* Upload flow integration
* URL generation

#### Custom Pipeline Steps
* Pricing pipeline steps
* Composition pipeline steps
* Checkout workflow steps

#### Custom Event Listeners
* Available events reference
* Async vs sync handlers
* Job dispatch from events

#### Custom CLI Commands
* Registering commands
* Accessing services
* Example: data sync command

#### API Reference
* OpenAPI spec location
* Using Swagger UI
* Authentication for testing

---

## 5. Format Guidelines

---

### All Guides

* Markdown format
* Task-oriented headings ("How to..." or verb-first)
* Code blocks with copy-paste commands
* Screenshots for admin UI (after Section 7 is complete)
* No assumed knowledge in merchant guide

---

### Cross-References

Guides link to each other:

```text
Merchant Guide → "See Deployment Guide for initial setup"
Deployment Guide → "See Merchant Guide for store configuration"
Developer Guide → "See API docs at /docs for endpoint reference"
```

---

## 6. Non-Goals (v0)

* No video tutorials
* No interactive tutorials
* No versioned documentation site (just markdown files)
* No API reference generator (OpenAPI spec serves this purpose)
* No FAQ (addressed organically in guides)
