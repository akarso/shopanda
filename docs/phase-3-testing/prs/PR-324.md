# PR-A — Admin Access And Permission Repair (Track 3)

## Summary

Closed the three highest-friction gaps in the admin panel identified in the Track 3 issue list:
credential prefill in the login form, the broken admin products grid (403), and missing
explanation for the display format setting.

Two of the three reported items (credential prefill and display format hint) were already
resolved in PR-307. This PR confirmed those resolutions, then focused on the remaining
permission boundary failure and the missing role guard.

## Scope

- internal/interfaces/http/admin/dist/admin.js
  - added `hasAdminPanelAccess(role)` — blocks non-admin roles from entering the shell after login
  - wired role check in the login success handler; shows "This account has no admin permissions." for ineligible roles
  - updated products grid 403 branch to show "Your account does not have products access." instead of the generic grid-load failure message
- internal/interfaces/http/auth.go
  - extended `meResponse` struct with `Role string \`json:"role"\``
  - `Me()` and `UpdateProfile()` handlers now return the role in the JSON response
- internal/interfaces/http/admin_handler_test.go
  - added assertions for admin role guard message and products permission error message in the embedded JS
- internal/interfaces/http/auth_test.go
  - `TestAuthHandler_Me_Success` asserts `role == "customer"` in `/auth/me` response
  - `TestAuthHandler_UpdateProfile_Success` asserts `role == "admin"` in `/auth/me/profile` response, matching the fixture customer role

## Behavior

### Role exposure in /auth/me and /auth/me/profile

Both endpoints now include a `role` field in the response envelope:

```json
{
  "data": {
    "id": "...",
    "email": "...",
    "first_name": "...",
    "last_name": "...",
    "status": "active",
    "role": "admin"
  }
}
```

### Admin login role gate

After a successful credential check, the login handler calls `hasAdminPanelAccess(role)` with
the role returned by `/auth/me`. Roles `admin`, `manager`, `editor`, and `support` are admitted.
Any other role (including `customer`) causes the token to be cleared and an explicit message to
be shown: "This account has no admin permissions."

### Products grid 403

When the products API returns `error.code === "forbidden"`, the grid now shows
"Your account does not have products access." instead of the generic "Failed to load grid or data."

## Why this slice

Track 3 requires admin access to be safe by default and functional for admin-role accounts.
Without the role gate, any registered customer account could attempt to use the admin shell.
Without the role field in `/auth/me`, the admin SPA had no signal to enforce the gate.

## Validation

- go test ./internal/interfaces/http -count=1 → PASS
- go build ./cmd/api → OK
