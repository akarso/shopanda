# PR-BATCH3-TESTS — Final Postgres Repo Integration Tests

## Summary

Integration tests for the last 6 untested postgres repositories: page, rewrite, consent, translation, content_translation, and asset.

## Files Created

| File | Tests | Covers |
|------|-------|--------|
| `page_repo_test.go` | 14 | CRUD, slug conflict, pagination, active filter, not-found, nil/empty guards |
| `rewrite_repo_test.go` | 7 | Save, upsert overwrite, find, delete, empty path, not-found, nil guard |
| `consent_repo_test.go` | 5 | Upsert insert/update, find, delete, not-found |
| `translation_repo_test.go` | 7 | Upsert insert/update, find, list by language, delete, `translation.ErrNotFound`, not-found |
| `content_translation_repo_test.go` | 8 | Upsert insert/update, find field value, find by entity+language (multiple + empty slice), delete, not-found |
| `asset_repo_test.go` | 4 | Save, find, JSONB meta+thumbnails roundtrip, not-found |

**Total: 45 new integration tests**

## Patterns Used

- `testDB(t)` + `ensureProductsTable(t, db)` for DB setup
- `mustExec(t, db, ...)` for defensive cleanup before seeding
- Defensive `DELETE FROM <table>` before count-sensitive tests
- `t.Cleanup(...)` for teardown
- Domain constructors for valid test data; pointer helpers returning `*T`
- `errors.Is(err, translation.ErrNotFound)` for sentinel error check
- `apperror.Is(err, apperror.CodeConflict/NotFound)` for apperror checks

## Test Results

```text
70 packages, 0 failures
```

## Coverage Notes

All postgres repo files now have integration tests. This completes the repository-level test coverage initiative.
