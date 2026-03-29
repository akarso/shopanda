# 📄 CMS — Content Pages Specification (v0)

## 1. Overview

Provides:

* simple content pages
* integration with URL system
* basic content management

---

## 2. Page Model

---

```go
type Page struct {
    ID       uuid.UUID
    Slug     string
    Title    string
    Content  string
    IsActive bool
}
```

---

---

## 3. Routing

---

Pages are resolved via URL system:

```text
/privacy-policy → page
```

---

---

## 4. Rendering

---

```text
resolve → load page → render template
```

---

---

## 5. Content Format

---

* HTML (default)

---

---

## 6. Admin Integration

---

Pages are managed via admin schema:

* create/edit page
* toggle active state

---

---

## 7. API

---

```http
GET /pages/{slug}
```

---

---

## 8. Constraints

---

* no layout system
* no visual builder
* no nested components

---

---

## 9. Security

---

* HTML must be sanitized
* prevent XSS

---

---

## 10. Summary

CMS v0 provides:

> a minimal, practical system for managing static content pages without introducing unnecessary complexity.

---
