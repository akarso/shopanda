# ⏱️ Scheduler / Cron System — v0 Specification

## 1. Overview

Scheduler provides:

* periodic job execution
* integration with job queue
* zero external dependencies

---

## 2. Design

---

Scheduler does NOT execute business logic directly.

Instead:

> scheduler → enqueue job → worker executes

---

---

## 3. Interface

---

```go
type Scheduler interface {
    Register(name string, spec string, job func())
}
```

---

---

## 4. Default Implementation

---

* in-process scheduler
* cron syntax support

---

---

## 5. Usage

---

```go
scheduler.Register("cache.cleanup", "*/5 * * * *", func() {
    queue.Enqueue("cache.cleanup", nil)
})
```

---

---

## 6. Plugin Integration

---

Plugins may register scheduled jobs in Init():

```go
app.Scheduler.Register(...)
```

---

---

## 7. CLI

---

```bash
app scheduler
```

---

---

## 8. Optional Integrations

---

* pg_cron (Postgres)
* external schedulers

---

---

## 9. Constraints

---

* scheduler must be lightweight
* jobs must be idempotent
* failures handled by queue system

---

---

## 10. Summary

Scheduler v0 provides:

> a simple, built-in cron system that integrates with the job queue and requires no additional infrastructure.

---
