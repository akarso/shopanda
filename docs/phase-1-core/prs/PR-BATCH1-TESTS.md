# PR-BATCH1 — Critical Test Gap Coverage (Phase 1)

## Summary

Closes 4 critical test gaps identified during the Phase 1 test audit: payment repo, shipping repo, account GDPR delete, and a full E2E cart→checkout smoke test.

## Changes

### New Files

| File | Purpose |
|------|---------|
| `internal/infrastructure/postgres/payment_repo_test.go` | 8 tests for `PaymentRepo`: nil DB, CRUD, find-by-order, not-found, empty-ID, duplicate conflict, update status, optimistic lock |
| `internal/infrastructure/postgres/shipping_repo_test.go` | 8 tests for `ShippingRepo`: nil DB, CRUD, find-by-order, not-found, empty-ID, duplicate conflict, update status, optimistic lock |
| `internal/application/account/service_test.go` | 7 tests for `Service.DeleteAccount`: happy path (consent + customer delete + event publish), consent-delete failure, customer-delete failure, 4 `NewService` nil-panic cases |
| `internal/interfaces/http/e2e_checkout_test.go` | 1 E2E smoke test: POST create cart → POST add item → POST checkout → verify order saved with correct customer, items, quantity |

## Test Inventory

| File | Tests | Type | Notes |
|------|-------|------|-------|
| `payment_repo_test.go` | 8 | Integration | 1 pass (NilDB), 7 skip without `SHOPANDA_TEST_DSN` |
| `shipping_repo_test.go` | 8 | Integration | 1 pass (NilDB), 7 skip without `SHOPANDA_TEST_DSN` |
| `service_test.go` | 7 | Unit | All pass — closure-based mocks, no DB |
| `e2e_checkout_test.go` | 1 | E2E | Full HTTP flow via `httptest`, stub repos, no DB |

**Total: 24 new tests, 70 packages pass, 0 failures.**

## Design Decisions

- **Repo tests use `testDB(t)` skip pattern**: consistent with all existing postgres test files — skips when `SHOPANDA_TEST_DSN` not set
- **Account mocks are closure-based**: `mockCustomerRepo{deleteFn: ...}` — matches project convention (no testify/gomock)
- **E2E test wires real handlers**: `CartHandler` + `CheckoutHandler` on a single `http.ServeMux` with `RequireAuth` middleware — tests the actual HTTP layer, not mocks
- **E2E uses stub repos**: in-memory maps implementing domain interfaces — tests handler+service integration without DB
- **Optimistic lock tests**: both repo files verify conflict detection via stale `updated_at` timestamps, matching the `WHERE updated_at = $old` pattern in the production code

## Gaps Closed

| Gap | Severity | File |
|-----|----------|------|
| `payment_repo.go` — 6 methods, 0 tests (checkout path) | Critical | `payment_repo_test.go` |
| `shipping_repo.go` — 6 methods, 0 tests (checkout path) | Critical | `shipping_repo_test.go` |
| `account/service.go` — `DeleteAccount` untested (GDPR) | Critical | `service_test.go` |
| No E2E cart→checkout smoke test (roadmap requirement) | Critical | `e2e_checkout_test.go` |

## Verification

```bash
go test ./internal/application/account/ -v
# 7 pass (3 delete tests + 4 panic subtests)

go test ./internal/interfaces/http/ -run TestE2E -v
# 1 pass (full cart→checkout flow)

go test ./internal/infrastructure/postgres/ -run "TestPaymentRepo|TestShippingRepo" -v
# 2 pass (NilDB), 14 skip (no DSN)

go test ./...
# 70 packages, 0 failures
```
