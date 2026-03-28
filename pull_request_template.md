# ✅ PR Review Checklist

## 1. Scope & Size

* [ ] PR introduces **one concept only**
* [ ] Changes are **≤ ~500 lines**
* [ ] Touches **≤ ~10 files**
* [ ] No unrelated refactoring included

---

## 2. Functionality

* [ ] Code **runs locally**
* [ ] Feature is **testable manually**
* [ ] No broken endpoints or flows
* [ ] No dead / unused code

---

## 3. Architecture Compliance

* [ ] Follows **hexagonal structure**

### Dependency rules:

* [ ] `domain` does NOT import infrastructure
* [ ] `domain` does NOT import interfaces
* [ ] `application` only orchestrates (no HTTP/DB logic)
* [ ] `interfaces` contain no business logic

---

## 4. Extensibility Rules (CRITICAL)

* [ ] No direct core overrides

* [ ] Uses proper extension mechanism:

  * [ ] event
  * [ ] pipeline
  * [ ] workflow
  * [ ] composition pipeline

* [ ] No hidden behavior (everything explicit)

---

## 5. Simplicity

* [ ] No premature abstractions
* [ ] No unused interfaces
* [ ] No “future-proofing” code
* [ ] Code is readable in one pass

---

## 6. Go Best Practices

* [ ] Interfaces used only at boundaries
* [ ] No unnecessary indirection
* [ ] Packages have single responsibility
* [ ] No “utils” dumping ground

---

## 7. Naming & Clarity

* [ ] Clear, descriptive names
* [ ] No abbreviations unless obvious
* [ ] Public vs private properly scoped

---

## 8. Error Handling

* [ ] Errors are handled or propagated
* [ ] No silent failures
* [ ] No panic in normal flow

---

## 9. Performance Awareness

* [ ] No obvious N+1 queries (if applicable)
* [ ] No unnecessary allocations / loops
* [ ] No blocking operations in wrong place

---

## 10. Security (Basic)

* [ ] No hardcoded secrets
* [ ] Input validation where needed
* [ ] No unsafe operations

---

## 11. Logging (Minimal for now)

* [ ] Only meaningful logs added
* [ ] No excessive debug noise

---

## 12. PR Description

* [ ] Explains **what** and **why**
* [ ] Mentions scope boundaries
* [ ] Mentions what is intentionally NOT included

---

## 13. Red Flags (Auto-Reject 🚫)

* [ ] Introduces DI container / framework
* [ ] Adds reflection-based magic
* [ ] Large multi-feature PR
* [ ] Breaks architecture rules
* [ ] Adds complexity without immediate need

---

## 14. Final Sanity Check

* [ ] Can reviewer understand this in **<15 minutes**?
* [ ] Would you approve this if you saw it in someone else’s repo?

---

## Guiding Rule

> If something feels “clever”, it’s probably wrong.
> If something feels obvious, it’s probably right.

---
