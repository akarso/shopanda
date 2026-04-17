# 🛠️ CLI Commands Specification (v0)

## 1. Overview

CLI provides operational control over the system.

It is used for:

* development
* deployment
* maintenance
* debugging

---

## 2. Entry Point

---

```bash id="c7g5z0"
app <command>
```

---

---

## 3. Core Commands

---

### Run API server

```bash id="9n6h8c"
app serve
```

---

### Run worker

```bash id="p9u4f3"
app worker
```

---

---

## 4. Database

---

### Run migrations

```bash id="r6x2dj"
app migrate
```

---

### Rollback (optional)

```bash id="z1h8yt"
app migrate:rollback
```

---

---

## 5. Seed Data

---

```bash id="4z6p1v"
app seed
```

---

Optional:

```bash id="u2m8s7"
app seed --env=dev
app seed --reset
```

---

---

## 6. Cache

---

### Clear all cache

```bash id="n2p6xt"
app cache:clear
```

---

### Clear by prefix

```bash id="2m7w1q"
app cache:clear --prefix=product_page
```

---

---

## 7. Search

---

### Reindex

```bash id="w1g7dp"
app search:reindex
```

---

---

## 8. Config

---

### Export config

```bash id="j9f3hx"
app config:export
```

---

### Import config

```bash id="k3m5ty"
app config:import config.yaml
```

---

---

## 9. Projections (Future)

---

```bash id="b8r1n0"
app projections:rebuild
```

---

---

## 10. Jobs / Queue

---

### Run worker

```bash id="u9v4oz"
app worker
```

---

### Retry failed jobs

```bash id="0r2d3e"
app jobs:retry
```

---

---

## 11. Plugins

---

### List plugins

```bash id="4y8x1v"
app plugins:list
```

---

---

## 12. System

---

### Health check

```bash id="5g7m2n"
app health
```

---

---

## 13. Dev Utilities (Optional)

---

### Reset database

```bash id="q8v3k1"
app dev:reset
```

---

---

## 14. Flags

---

### Common flags

```bash id="l2c4d6"
--env=dev|prod
--config=path
--verbose
```

---

---

## 15. Command Design Rules

---

* commands must be explicit
* no hidden side effects
* idempotent where possible
* safe for repeated execution

---

---

## 16. Output

---

* human-readable by default
* JSON output optional (future)

---

---

## 17. Non-Goals (v0)

---

* no interactive CLI
* no wizard-style setup
* no complex scripting DSL

---

---

## 18. Summary

CLI provides:

> a simple and consistent interface to control all system operations.

---
