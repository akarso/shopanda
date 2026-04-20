# PR-BATCH2-TIER2-TESTS — Postgres Repo Tests (Batch 2 Tier 2+3)

## Summary

Integration tests for 6 previously untested postgres repositories across Tier 1 (Critical) and Tier 2 (High) priorities.

## Files Created

| File | Repo | Tests |
|------|------|-------|
| `category_repo_test.go` | CategoryRepo | 13 |
| `store_repo_test.go` | StoreRepo | 12 |
| `collection_repo_test.go` | CollectionRepo | 14 |
| `tax_rate_repo_test.go` | TaxRateRepo | 8 |
| `price_history_repo_test.go` | PriceHistoryRepo | 5 |
| `reset_token_repo_test.go` | ResetTokenRepo | 6 |

**Total: 58 tests across 6 files**

## Test Coverage by Repo

### CategoryRepo (13 tests)
- NilDB, Create+FindByID, FindBySlug, FindByID_NotFound, FindByID_EmptyID, FindBySlug_EmptySlug, FindByParentID_Roots, FindByParentID_Children, Update, Update_NotFound, Create_DuplicateSlug (Conflict), FindAll, Create_Nil

### StoreRepo (12 tests)
- NilDB, Create+FindByID, FindByCode, FindByDomain, FindDefault, FindAll, Update, Update_NotFound, Create_DuplicateCode (Conflict), FindByID_EmptyID, FindByID_NotFound, Create_Nil

### CollectionRepo (14 tests)
- NilDB, Create+FindByID, FindBySlug, List, Update, Update_NotFound, Create_DuplicateSlug (Conflict), AddProduct (with FK product seed), AddProduct_Duplicate (Conflict), RemoveProduct, RemoveProduct_NotFound, FindByID_EmptyID, FindByID_NotFound, Create_Nil, ListProductIDs_EmptyID

### TaxRateRepo (8 tests)
- NilDB, Upsert+Find, Upsert_Update (same tuple returns original ID), ListByCountry, Delete, Delete_NotFound, Find_NotFound, Upsert_Nil

### PriceHistoryRepo (5 tests)
- NilDB, Record+LowestSince, LowestSince_ReturnsMin (3 records, picks lowest), LowestSince_NotFound (nil,nil), Record_Nil

### ResetTokenRepo (6 tests)
- NilDB, Create+FindByTokenHash, FindByTokenHash_NotFound, MarkUsed (verifies UsedAt set), MarkUsed_AlreadyUsed (TOCTOU-safe, returns NotFound), MarkUsed_NotFound

## Patterns Used

- `testDB(t)` + `ensureProductsTable(t, db)` for DB setup
- Defensive `DELETE FROM` before seeding in tests asserting exact counts
- Raw SQL `INSERT INTO products` for FK seeding in collection AddProduct tests
- `t.Cleanup()` for teardown
- `apperror.Is(err, apperror.Code*)` for error assertions
- `shared.MustNewMoney()` for price history snapshots

## Build & Test

```text
go build ./...  → clean
go test ./...   → 70 packages, 0 failures
```
 