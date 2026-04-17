# 🔄 Checkout Workflow Specification

## 1. Overview

The checkout process is implemented as a **deterministic, extensible workflow pipeline**.

It replaces:

* hardcoded checkout logic
* controller spaghetti
* fragile overrides

Design goals:

* extensibility (plugins can add/modify steps)
* predictability (clear execution order)
* safety (idempotent, retryable)
* observability (traceable execution)

### ⚠️ Order Creation Rule

Order creation is part of the checkout workflow.

> Orders MUST NOT be created directly via API outside of checkout.

This ensures:
- consistent validation
- correct pricing and inventory handling
- proper payment initialization

---

## 2. Core Concepts

---

### 2.1 Checkout Context

Central object passed through all steps.

```go
type CheckoutContext struct {
    CartID      string
    Cart        *Cart

    CustomerID  string

    Currency    string

    Order       *Order

    Meta        map[string]interface{}

    Errors      []error
}
```

---

### 2.2 Checkout Step

Each step is a unit of work.

```go
type CheckoutStep interface {
    Name() string
    Execute(ctx *CheckoutContext) error
}
```

---

## 3. Default Workflow (MVP)

```text
1. validate_cart
2. recalculate_pricing
3. reserve_inventory
4. create_order
5. initiate_payment
```

---

## 4. Step Responsibilities

---

### 4.1 validate_cart

* ensure cart exists
* validate items (existence, quantity)
* basic business rules

---

### 4.2 recalculate_pricing

* execute pricing pipeline
* update totals

---

### 4.3 reserve_inventory

* create reservations
* fail if insufficient stock

---

### 4.4 create_order

* persist order + items
* snapshot pricing

⚠️ This is the ONLY place where orders are created in the system.

---

### 4.5 initiate_payment

* call payment provider
* return payment instructions

---

## 5. Execution Model

* steps execute sequentially
* failure stops execution
* no implicit retries (handled externally)

---

## 6. Step Registration

Plugins can modify workflow.

---

### 6.1 Register Step

```go
RegisterCheckoutStep(step CheckoutStep, position string)
```

---

### Position options:

* `before:<step>`
* `after:<step>`
* `start`
* `end`

---

### Example:

```go
RegisterCheckoutStep(MyStep{}, "before:initiate_payment")
```

---

## 7. Conditional Execution

Steps may choose to skip:

```go
func (s MyStep) Execute(ctx *CheckoutContext) error {
    if !shouldRun(ctx) {
        return nil
    }
    ...
}
```

---

## 8. Idempotency

Steps MUST be idempotent.

Examples:

* do not create duplicate orders
* do not double-reserve inventory

---

### Recommended:

* store markers in `ctx.Meta`
* check existing state before acting

---

## 9. Error Handling

---

### Step failure:

* stops workflow
* returns error to caller

---

### Error types:

* validation error → user-facing
* system error → retryable/logged

---

## 10. Compensation (Rollback)

Workflow does NOT auto-rollback.

Instead:

* each step must be reversible or safe
* failures trigger compensating actions via events

---

### Example:

* reservation created → on failure → emit `inventory.release`

---

## 11. Events Integration

Each step emits events:

```text
checkout.step.started
checkout.step.completed
checkout.failed
checkout.completed
```

---

### Example:

```go
Emit("checkout.step.started", step.Name())
```

---

## 12. Observability

Workflow should produce execution trace:

```json
[
  { "step": "validate_cart", "status": "ok" },
  { "step": "reserve_inventory", "status": "ok" }
]
```

---

## 13. Extensibility Examples

---

### Add custom validation

```go
RegisterCheckoutStep(CustomValidation{}, "after:validate_cart")
```

---

### Add legal disclaimer step

```go
RegisterCheckoutStep(DisclaimerCheck{}, "before:create_order")
```

---

### Add fraud check

```go
RegisterCheckoutStep(FraudCheck{}, "before:initiate_payment")
```

---

## 14. Step Categories (Convention)

---

### Validation steps

* run early
* can block flow

---

### Mutation steps

* modify state (order, inventory)

---

### Integration steps

* external calls (payments, fraud)

---

## 15. Concurrency & Safety

* workflow runs per checkout request
* no shared mutable state
* must tolerate retries

---

## 16. Future Extensions

* async steps (background processing)
* step retries
* distributed workflows

---

## 17. Non-Goals

* no BPM engine
* no visual workflow builder
* no complex branching logic (initially)

---

## 18. Summary

Checkout workflow is:

> a structured, extensible pipeline of steps that transforms a cart into an order.

Order creation is an internal step of checkout and cannot be triggered independently.

It enables:

* custom checkout logic
* legal compliance flows
* integrations
* extensibility without core modification

---
