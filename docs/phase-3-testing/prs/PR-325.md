# PR-B — Admin Shell And Navigation Restructure (Track 3)

## Summary

Implemented the next Track 3 slice by restructuring the admin shell navigation around business domains from `ADMIN_IMPROVEMENTS.md`, while keeping implementation scope minimal:

- no backend contract changes
- no data model changes
- existing working pages preserved
- non-implemented sections routed to explicit placeholders

## Scope

- internal/interfaces/http/admin/dist/index.html
  - replaced flat sidebar links with grouped domain navigation sections:
    - Sales
    - Catalog
    - Customers
    - Marketing
    - Content
    - Operations
    - Settings
    - Store Management
    - Integrations
- internal/interfaces/http/admin/dist/admin.js
  - added route entries for grouped navigation paths
  - routed not-yet-implemented pages to `renderPlaceholder(...)`
  - kept existing implemented pages wired:
    - Dashboard
    - Orders
    - Products
    - Media
    - Settings (General)
    - Account
  - simplified sidebar active-state logic to generic path-prefix matching
- internal/interfaces/http/admin/dist/admin.css
  - added grouped nav styling (`.nav-group`, `.nav-group-label`)
- internal/interfaces/http/admin_handler_test.go
  - updated static asset assertions for grouped nav labels and links
  - added CSS assertion for grouped nav styles

## Behavior

### Domain-first nav shell

The admin sidebar now reflects operator intent rather than flat resource links.

### Explicit placeholders

Unimplemented pages do not 404 in the SPA shell; they render a clear placeholder message and keep navigation structure stable for future slices.

### Active link behavior

Sidebar highlight is now based on route-prefix semantics for all links, reducing per-route special-case logic.

## Why this slice

Track 3 requires a domain-driven shell before introducing context switching and scoped editing. This PR establishes that shell without broadening permissions, changing APIs, or coupling to future scope contracts.

## Validation

- go test ./internal/interfaces/http -run 'TestAuthHandler_UpdateProfile_Success|TestAdminHandler' -count=1
- go test ./internal/interfaces/http/admin_handler_test.go (via test runner)
- go build ./cmd/api

All pass.

## Notes

- This slice intentionally does not add context switchers yet.
- This slice intentionally does not change admin endpoint permissions.
- Follow-up slices (PR-C and PR-D) should layer context and scoped contracts onto this shell.
