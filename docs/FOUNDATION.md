# 🧱 Founding Document — (Working Name)

## 1. Vision

Build a **developer-first ecommerce engine** that combines:

* the **extensibility of Magento**
* the **simplicity of Shopware**
* the **performance and minimalism of Go + PostgreSQL**

Delivered as:

> A **drop-in, event-driven ecommerce backend** that runs as a single binary and scales when needed.

---

## 2. Core Principles

### 2.1 Simplicity First

* The system must be understandable in hours, not weeks
* No hidden magic, no implicit behavior
* Prefer explicit over clever

---

### 2.2 Minimal Dependencies

* No dependency-heavy ecosystems (no Composer, no node_modules in core)
* Standard library first approach
* Every dependency must justify its existence

---

### 2.3 Binary-First Deployment

* The system compiles into a **single executable**
* Must run without containers
* Containers (Docker) are optional, not required

---

### 2.4 Postgres as the Engine

* PostgreSQL is a **first-class component**, not just storage
* Use:

  * JSONB for extensibility
  * Full-text search
  * Triggers and views where appropriate
* Avoid unnecessary service proliferation

---

### 2.5 Event-Driven by Default

* Every meaningful action emits an event
* Internal communication prefers events over direct coupling
* Must work:

  * in-process (default)
  * with external brokers (optional)

---

### 2.6 Hexagonal Architecture

* Core domain logic is isolated from:

  * transport (HTTP, CLI)
  * storage
  * external services
* Infrastructure is replaceable

---

### 2.7 Extensibility Without Chaos

The system must support multiple extension strategies:

1. **Event subscribers** (safe, async)
2. **Service overrides** (controlled replacement)
3. **Pipeline hooks** (pre/post execution control)

Goal:

> Match Magento-level flexibility without Magento-level complexity.

---

### 2.8 Drop-in Usability

The system must work out of the box:

```bash
git clone <repo>
cd project
./app migrate
./app seed
./app serve
```

Result:

* running API
* working checkout
* sample data

---

### 2.9 Device Agnostic

* System is API-first.
* Server-side rendering (SSR) is provided as a default client of the application layer.
* SSR can be disabled, making the system fully headless without changes to core logic.
* No frontend lock-in
* Works with:

  * web
  * mobile
  * headless clients

---

### 2.10 Frontend Philosophy

* No framework dependency required
* Must work with:

  * pure CSS
  * pure JavaScript (Web Components encouraged)
* Optional support for SCSS:

  * SCSS is allowed but not required
  * system must function fully with plain CSS

---

### 2.11 Local-First Development

* Must run locally with minimal setup
* No cloud dependencies required
* Developer experience is a priority

---

### 2.12 Operational Simplicity

* Must run on a single low-cost VPS
* Scaling should be incremental:

  * single instance → multiple instances → distributed system
* No Kubernetes requirement

---

## 3. System Boundaries

### Core Domains (MVP)

* Catalog (products, variants)
* Pricing
* Inventory
* Cart
* Orders
* Customers

---

### Out of Scope (MVP)

* Advanced CMS
* Complex promotion engines
* Enterprise workflows

---

## 4. Architecture Overview

* Core: domain logic (pure)
* Adapters:

  * HTTP API
  * CLI
* Infrastructure:

  * PostgreSQL
  * optional Redis
  * optional message broker

---

## 5. Plugin System (First-Class Feature)

Plugins must be able to:

* subscribe to events
* override services
* inject into pipelines

Plugins must:

* be isolated
* be explicitly registered
* not rely on hidden global state

---

## 6. Non-Goals

* Not a SaaS platform
* Not a low-code/no-code tool
* Not optimized for non-technical users
* Not an enterprise-first system

---

## 7. Target Users

* Developers
* Technical founders
* Agencies building custom ecommerce
* Teams outgrowing SaaS platforms

---

## 8. Success Criteria

The system is successful if:

* A developer can run it in minutes
* A store can go live without rewriting core logic
* Custom features can be added without hacking internals
* Infrastructure remains simple even as complexity grows

---

## 9. Guiding Philosophy

> Build the simplest system that can grow into a complex one—
> not a complex system that tries to feel simple.

---
