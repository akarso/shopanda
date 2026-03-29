# 🧾 Invoicing System — v0 Specification

## 1. Overview

Provides:

* invoice generation
* correction invoices (credit notes)
* PDF export
* integration with orders and mailer

---

## 2. Model

---

### Invoice

* immutable financial document
* snapshot of order

---

### Credit Note

* used for corrections
* linked to original invoice

---

---

## 3. Lifecycle

---

```text
order → payment → invoice → email
```

---

---

## 4. Numbering

---

* sequential numbering required
* generated via DB sequence

---

---

## 5. PDF

---

* generated from HTML template
* stored for retrieval

---

---

## 6. Events

---

* invoice.created
* credit_note.created

---

---

## 7. Integration

---

* linked to order
* triggers mailer

---

---

## 8. Constraints

---

* invoices immutable
* no recalculation
* corrections via credit notes only

---

---

## 9. Summary

Invoicing system provides:

> a legally compliant, immutable, and extensible solution for generating and managing invoices.

---
