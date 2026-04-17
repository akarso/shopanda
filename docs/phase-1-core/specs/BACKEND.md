# 🧱 Backend Guide — Implementation & Structure (BACKEND.md, v0)

## 1. Overview

This document defines:

* backend structure
* where code should live
* how to implement features consistently

It complements architecture specs by focusing on **practical implementation rules**.

---

## 2. Project Structure

---

```plaintext
/internal
  /domain
  /application
  /interfaces
  /infrastructure

/cmd/api
```

---

---

## 3. Layer Responsibilities

---

### Domain

* business logic
* entities
* invariants

---

Rules:

* ❌ no HTTP
* ❌ no database queries
* ❌ no external dependencies

---

---

### Application

* orchestrates use cases
* coordinates domain + infrastructure

---

Rules:

* ❌ no HTTP handling
* ❌ no direct SQL
* ✅ calls repositories/services

---

---

### Interfaces

* HTTP handlers
* request/response mapping

---

Rules:

* ❌ no business logic
* ❌ no direct DB access

---

---

### Infrastructure

* database (Postgres)
* external services
* plugins

---

Rules:

* implements interfaces
* contains adapters

---

---

## 4. Data Flow

---

### Write (Command)

```text
HTTP → handler → application → domain → repository → DB
```

---

---

### Read (Query)

```text
HTTP → handler → composition pipeline → repository → DB
```

---

👉 Composition pipeline builds response.

---

---

## 5. Core Patterns

---

### 5.1 Pipelines

Used for:

* pricing
* transformations

---

### 5.2 Workflows

Used for:

* checkout
* order lifecycle

---

### 5.3 Composition

Used for:

* API responses (PDP, PLP)

---

### 5.4 Events

Used for:

* async reactions
* decoupling

---

---

## 6. Adding a New Feature

---

### Example: new endpoint

---

#### Step 1: Handler

```plaintext
/interfaces/http/handlers
```

---

#### Step 2: Application logic

```plaintext
/application
```

---

#### Step 3: Domain logic

```plaintext
/domain
```

---

#### Step 4: Repository (if needed)

```plaintext
/infrastructure
```

---

---

## 7. Repositories

---

Define interfaces in domain/application:

```go
type ProductRepository interface {
    GetByID(id string) (*Product, error)
}
```

---

Implement in infrastructure:

```go
type PostgresProductRepository struct{}
```

---

---

## 8. Database Access

---

Rules:

* only infrastructure touches SQL
* no SQL in application/domain

---

---

## 9. Error Handling

---

* use standard error codes
* propagate errors upward
* no silent failures

---

---

## 10. Logging

---

* use structured logging
* log events, not steps

---

---

## 11. Configuration

---

Access via:

```go
config.Get("key")
```

---

---

## 12. Plugins

---

Plugins:

* register extensions in Init()
* must not bypass architecture

---

---

## 13. Testing (Basic)

---

* domain → unit tests
* application → flow tests

---

---

## 14. Anti-Patterns

---

❌ business logic in handlers
❌ SQL in domain
❌ global state
❌ hidden side effects

---

---

## 15. Guiding Principle

---

> Keep logic where it belongs.
> If unsure — it probably belongs in domain or application.

---
