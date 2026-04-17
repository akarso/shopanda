# 🗂️ Project Structure & Guidelines

## 1. Overview

The project follows:

* **Hexagonal Architecture**
* **Domain-first design**
* **Minimal framework usage**
* **Clear separation of concerns**

Goals:

* easy to navigate
* easy to extend
* hard to misuse

---

## 2. High-Level Structure

```plaintext
/app
  /cmd            # entrypoints (api, worker, cli)
  /internal       # private application code
  /pkg            # shared utilities (optional)
  /plugins        # external plugins (runtime)
  /migrations     # database migrations
  /configs        # config files
```

---

## 3. Entry Points (`/cmd`)

```plaintext
/cmd
  /api
    main.go
  /worker
    main.go
  /cli
    main.go
```

### Responsibilities:

* bootstrap application
* wire dependencies
* start services

👉 No business logic here.

---

## 4. Core Application (`/internal`)

```plaintext
/internal
  /domain
  /application
  /infrastructure
  /interfaces
  /platform
```

---

## 5. Domain Layer (`/internal/domain`)

Pure business logic.

```plaintext
/domain
  /product
  /cart
  /order
  /inventory
  /pricing
```

Each domain:

```plaintext
/product
  entity.go
  repository.go
  events.go
```

### Rules:

* no external dependencies
* no DB code
* no HTTP
* emits events only

---

## 6. Application Layer (`/internal/application`)

Use cases / orchestration.

```plaintext
/application
  /cart
    service.go
  /order
    service.go
```

### Responsibilities:

* coordinate domains
* call repositories
* trigger events
* execute pricing pipeline

---

## 7. Infrastructure Layer (`/internal/infrastructure`)

Concrete implementations.

```plaintext
/infrastructure
  /db
    postgres/
  /cache
    redis/
  /events
    bus.go
  /plugins
    manager.go
```

### Responsibilities:

* DB access
* external systems
* plugin execution
* message brokers

---

## 8. Interfaces Layer (`/internal/interfaces`)

External entrypoints.

```plaintext
/interfaces
  /http
    /handlers
    /middleware
    router.go
  /cli
```

### Responsibilities:

* HTTP handlers
* request/response mapping
* validation

👉 No business logic here.

---

## 9. Platform Layer (`/internal/platform`)

Cross-cutting concerns.

```plaintext
/platform
  logger/
  config/
  id/
  time/
```

### Responsibilities:

* utilities
* shared services
* helpers

---

## 10. Plugins Directory (`/plugins`)

Runtime plugins.

```plaintext
/plugins
  /stripe
    plugin
    manifest.json
  /tax
    plugin
    manifest.json
```

---

## 11. Migrations (`/migrations`)

```plaintext
/migrations
  001_init.sql
  002_cart.sql
```

* pure SQL
* versioned
* no ORM magic

---

## 12. Configs (`/configs`)

```plaintext
/configs
  config.yaml
```

---

## 13. Dependency Rules (CRITICAL)

---

### Allowed Direction:

```text
interfaces → application → domain
                ↓
         infrastructure
```

---

### Forbidden:

* domain importing infrastructure ❌
* domain importing interfaces ❌
* circular dependencies ❌

---

## 14. Package Design Rules

---

### Keep packages small

* one responsibility
* clear naming

---

### Avoid "utils" dumping ground

If something grows:
→ give it a proper package

---

### Prefer interfaces at boundaries

Example:

```go
type ProductRepository interface {
    FindByID(id string) (*Product, error)
}
```

---

## 15. Event System Placement

```plaintext
/domain/.../events.go
/infrastructure/events/bus.go
```

* domain defines events
* infrastructure delivers them

---

## 16. Pricing Pipeline Placement

```plaintext
/domain/pricing/
/application/pricing/
/infrastructure/pricing/
```

* domain: models
* application: orchestration
* infra: plugin steps

---

## 17. Plugin System Placement

```plaintext
/infrastructure/plugins/
```

Handles:

* discovery
* lifecycle
* communication

---

## 18. Testing Strategy

---

### Domain tests

* pure unit tests
* no mocks needed

---

### Application tests

* mock repositories
* test flows

---

### Integration tests

* DB + API

---

## 19. CLI Tooling

Expose:

```bash
app serve
app migrate
app seed
app plugins:list
```

---

## 20. Design Principles

---

### 1. Explicit over magic

No hidden behavior.

---

### 2. Composition over inheritance

Prefer wiring over deep hierarchies.

---

### 3. Core stays small

Push complexity to plugins.

---

### 4. Readability > cleverness

Future you must understand it quickly.

---

## 21. Anti-Patterns to Avoid

---

### ❌ God packages

```plaintext
/services
/helpers
/utils
```

---

### ❌ Leaky abstractions

* DB logic in domain
* HTTP logic in application

---

### ❌ Over-engineering

* unnecessary interfaces
* premature microservices

---

## 22. Summary

This structure ensures:

* clean separation of concerns
* strong domain boundaries
* easy extensibility
* maintainable growth

> If the structure is respected, the system will remain simple—even as features grow.

---
