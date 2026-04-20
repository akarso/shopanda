# PR-BATCH2-TIER1 — Checkout-Critical Postgres Repo Tests (Phase 1)

## Summary

Adds integration tests for 5 Tier 1 (checkout-critical) postgres repos identified as having zero test coverage during the Phase 1 test audit: invoice, credit note, coupon, promotion, and shipping zone/rate tier.

## Changes

### New Files

| File | Purpose |
|------|---------|
| `internal/infrastructure/postgres/invoice_repo_test.go` | 8 tests for `InvoiceRepo`: nil DB, save+find-by-ID (with items), find-by-order-ID, not-found, empty-ID×2, save-nil, multiple items |
| `internal/infrastructure/postgres/credit_note_repo_test.go` | 7 tests for `CreditNoteRepo`: nil DB, save+find-by-ID (with items), find-by-invoice-ID (multi), not-found, empty-ID×2, save-nil |
| `internal/infrastructure/postgres/coupon_repo_test.go` | 9 tests for `CouponRepo`: nil DB, save+find-by-ID, find-by-code, find-by-code not-found, list-by-promotion, upsert, delete, delete not-found, save-nil |
| `internal/infrastructure/postgres/promotion_repo_test.go` | 9 tests for `PromotionRepo`: nil DB, save+find-by-ID, not-found, list-active (type filter + priority ordering), inactive-excluded, upsert, delete, delete not-found, save-nil |
| `internal/infrastructure/postgres/zone_repo_test.go` | 17 tests for `ZoneRepo`: nil DB, create+find-by-ID, not-found, empty-ID, list-zones, update, update not-found, delete, delete not-found, duplicate conflict, create-rate-tier+find, list-rate-tiers, update-rate-tier, delete-rate-tier, delete-rate-tier not-found, invalid-zone FK, create-nil |

## Test Inventory

| File | Tests | Type | Notes |
|------|-------|------|-------|
| `invoice_repo_test.go` | 8 | Integration | 1 pass (NilDB), 7 skip without `SHOPANDA_TEST_DSN` |
| `credit_note_repo_test.go` | 7 | Integration | 1 pass (NilDB), 6 skip without `SHOPANDA_TEST_DSN` |
| `coupon_repo_test.go` | 9 | Integration | 1 pass (NilDB), 8 skip without `SHOPANDA_TEST_DSN` |
| `promotion_repo_test.go` | 9 | Integration | 1 pass (NilDB), 8 skip without `SHOPANDA_TEST_DSN` |
| `zone_repo_test.go` | 17 | Integration | 1 pass (NilDB), 16 skip without `SHOPANDA_TEST_DSN` |

**Total: 50 new tests, 70 packages pass, 0 failures.**

## Design Decisions

- **Reuses existing `testDB(t)` + `ensureProductsTable(t, db)` helpers**: migrations run once per test via shared helper in `product_repo_test.go`; each test file adds its own `t.Cleanup` for table-specific data
- **Domain constructors used throughout**: `invoice.NewInvoice`, `invoice.NewItem`, `invoice.NewCreditNote`, `promotion.NewPromotion`, `promotion.NewCoupon`, `shipping.NewZone`, `shipping.NewRateTier` — no raw struct literals
- **Upsert tests verify mutation**: coupon and promotion tests save, mutate fields, save again, then read back to confirm the update took effect
- **Zone repo tests cover error classification**: duplicate create → `apperror.Conflict`, update/delete non-existent → `apperror.NotFound`, rate tier with invalid zone FK → `apperror.Validation`
- **Invoice/credit note tests verify items round-trip**: save with line items, read back, assert item count and SKU values match

## Gaps Closed

| Gap | Severity | File |
|-----|----------|------|
| `invoice_repo.go` — 3 methods, 0 tests (financial documents) | Critical | `invoice_repo_test.go` |
| `credit_note_repo.go` — 3 methods, 0 tests (refund path) | Critical | `credit_note_repo_test.go` |
| `coupon_repo.go` — 6 methods, 0 tests (cart pricing) | Critical | `coupon_repo_test.go` |
| `promotion_repo.go` — 5 methods, 0 tests (cart pricing) | Critical | `promotion_repo_test.go` |
| `zone_repo.go` — 11 methods, 0 tests (shipping calculation) | Critical | `zone_repo_test.go` |

## Verification

```bash
go test ./internal/infrastructure/postgres/ -run "TestInvoiceRepo|TestCreditNoteRepo|TestCouponRepo|TestPromotionRepo|TestZoneRepo" -v
# 5 pass (NilDB), 45 skip (no DSN)

go test ./...
# 70 packages, 0 failures
```
 