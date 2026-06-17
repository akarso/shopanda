# Phase 3 — Runtime Refactor Roadmap

## Strategy

* Track larger runtime and behavioral refactors separately from narrow bug-fix PRs
* Keep each implementation PR scoped to one production behavior change, even when it advances a larger refactor track
* Prefer vertical slices that fix the user-visible inconsistency first, then widen into domain changes only when required

---

## Refactor Tracks

| Track | Status | Goal | Notes |
| --- | --- | --- | --- |
| 1 | done in PR-306 | Guest cart continuity across login/register | Anonymous cart is claimed or merged into the authenticated customer's active cart, and the guest cart cookie is cleared so storefront surfaces stay in sync |
| 2 | done (PR-318–321, PR-393, PR-394) | Guest checkout without account creation | Guest checkout, `contact_email`, claim/link APIs, guest notifications, the `/account/orders/claim` page, and Postgres link persistence all shipped |
| 3 | done (PR-322–397) | Admin bootstrap, permissions, and usability hardening | Shell, context switcher, scoped settings, audit slices, admin surfaces, PR-E catalog + content (pages) scoped editing, removal of `changeme` bootstrap defaults, and audit coverage for category/store/media mutations all shipped |
| 4 | done (PR-309–317, PR-398, PR-399) | Customer account UX and account-security hardening | Header account entry, profile/security split, step-up auth, email verification, profile-side addresses/preferences self-service (with checkout prefill), and step-up-gated account email change with re-verification of the new address all shipped |

---

## Closing Plan — Remaining PRs

Verified against the codebase as of PR-392. Each PR below has a planned spec under `prs/`.

| PR | Track | Scope |
| --- | --- | --- |
| PR-393 | 2 | Guest order notification and confirmation parity: notifications/invoices fall back to `contact_email`, fix guest confirmation page, remove stale auth-gate template copy |
| PR-394 | 2 | Guest order claim end-to-end: persist `customer_id` on link in Postgres, wire claim search to discovery, add the `/account/orders/claim` page the claim email already links to |
| PR-395 | 3 | Admin scoped product editing (PR-E catalog): translatable fields per language via `content_translations`, store-scoped pricing, scope badges driven by the context switcher |
| PR-396 | 3 | Admin pages editing with language scope (PR-E content): pages CRUD UI over the existing API, content-domain permissions for the Editor role, page audit logging |
| PR-397 | 3 | Admin hardening closeout: remove `changeme` bootstrap defaults, fix stale seeding docs, add audit logging to categories/stores/media handlers |
| PR-398 | 4 | Storefront profile completion: saved addresses with checkout prefill, preferences page surfacing the existing consent API |

### Explicitly Deferred (out of roadmap closure)

* REST `/api/v1/checkout` guest parity for headless clients — storefront SSR covers the Track 2 validation target
* Store Management structural CRUD relocation — store editing works under Settings → General; moving it is a UI reshuffle without behavior gain
* Admin user/role management beyond the read-only Users & Roles surface — static RBAC is the accepted model
* Login-time 2FA / TOTP — registration email verification plus step-up re-auth is the accepted Track 4 model
* Placeholder admin sections (Marketing, Returns, Transactions, Attributes, Customer Groups, Inventory, Navigation, Blocks) — new features, not part of this roadmap's hardening goals
* Persistent audit table and audit browsing UI — audit stays logger-based for now

---

## Track 2 — Guest Checkout Without Account

### Goal

Allow an anonymous shopper to move from cart to completed order without being forced to create or log into an account.

### Why This Is Separate

Track 1 only fixes cart ownership continuity. Track 2 changes checkout and order semantics:

* checkout requires a non-empty `customerID`
* order creation requires a non-empty `customerID`
* storefront checkout marks authentication as required before confirmation

That is a broader domain refactor than the guest-cart handoff and should stay isolated from it.

### Expected Scope

* checkout service accepts guest checkout inputs without a customer identity
* order domain and persistence stop assuming every order has a registered customer id
* storefront checkout pages remove the hard account gate and switch to guest-capable messaging and validation
* notification and account flows define how a guest order can later be attached to an account or discovered safely
* operational logging and tests cover both guest and authenticated checkout paths

### Design Constraints

* guest checkout must not break existing authenticated checkout
* account-based order history must remain correct for registered users
* guest orders need a recoverable identity model, likely based on email plus explicit claim or link flows rather than implicit account creation
* cart ownership and order ownership must remain explicit; no hidden reassignment across customers

### Open Questions

* should guest checkout create a lightweight customer record, or should orders support `customer_id` being empty?
* what is the post-purchase account-linking flow: passwordless claim, explicit registration, or manual merge?
* what is the minimum guest identity snapshot required on the order for support and notification flows?

### Validation Target

When Track 2 ships, the same catalog/cart should support both:

* anonymous shopper: cart -> checkout -> order confirmation
* authenticated shopper: cart -> checkout -> order confirmation

without divergent pricing, stock reservation, or notification behavior.

---

## Track 3 — Admin Bootstrap, Permissions, And Usability Hardening

### Goal

Make first-boot admin access safe by default, restore expected admin API and grid access, and close the highest-friction gaps in the admin UI.

### Design Source

Track 3 should use `docs/phase-3-testing/specs/ADMIN_IMPROVEMENTS.md` as the design contract for the broader admin refactor. That spec adds the longer-term direction beyond the immediate bug list:

* domain-first navigation instead of database-table navigation
* one visible global context switcher for store, language, and currency
* a simple override model with global values plus store-level overrides only
* a hard separation between structural store management and behavioral feature configuration
* a scoped admin that stays explicit, predictable, and smaller than Magento-style configuration systems

### Reported Issues To Track

* `/admin` comes with a prefilled username and password; even if the project keeps a default bootstrap admin, credentials must not be exposed or auto-filled in the login UI
* seeded products appear on the storefront, but the admin products grid only shows `Failed to load grid or data.`
* `https://shopanda.eu/api/v1/admin/products?page=1&per_page=20&sort=created_at&order=desc` returns `{"data":null,"error":{"code":"forbidden","message":"insufficient permissions"}}`, and there are broader 403 failures across admin endpoints that need a single permissions review instead of endpoint-by-endpoint patching
* there is no admin account edit page
* Settings -> display format uses `{currency} {amount}` without enough explanation and needs at least a hint that makes the placeholder format understandable

### Expected Scope

* remove credential prefill from the admin login flow and decide how bootstrap admin credentials are delivered safely on first boot
* trace the admin auth and authorization path from session/token issuance through permission checks and fix the product grid/API failures at the owning boundary
* add a minimal admin account edit surface for the current authenticated admin user
* improve settings form copy so display format placeholders are explained in-context
* restructure the admin information architecture around business domains such as Sales, Catalog, Customers, Marketing, Content, Operations, Settings, Store Management, and Integrations
* introduce explicit store/language/currency context selection and the backing API contracts needed to make scope visible instead of implicit
* define a scoped data model for admin editing so fields declare whether they are global, translatable, or store-specific
* move feature configuration closer to the owning feature area and keep Store Management focused on structural entities such as stores, domains, languages, and currencies

### Design Constraints

* bootstrap and recovery flows must stay operator-friendly without leaking secrets into rendered pages or browser autofill defaults
* admin permission fixes should restore legitimate access without widening privileges beyond the intended admin role model
* admin UI additions should stay small and focused rather than turning into a broad settings redesign
* admin navigation should stay within three levels: top level, section, page
* the admin must not split into separate UIs per store; one UI plus explicit context switching is the target model
* avoid hidden inheritance, fallback trees, or a configuration graveyard under Settings

### Proposed PR Breakdown

#### PR-A — Admin Access And Permission Repair

* remove prefilled admin credentials from `/admin`
* fix the broken admin products grid and the product endpoint 403 path at the owning auth/permission boundary
* review other erroneous admin 403s as one permission slice instead of scattered endpoint fixes
* add the missing hint for display format placeholders where the current `{currency} {amount}` format is edited

#### PR-B — Admin Shell And Navigation Restructure

* implement the top-level admin information architecture from `ADMIN_IMPROVEMENTS.md`
* group navigation by domain: Sales, Catalog, Customers, Marketing, Content, Operations, Settings, Store Management, Integrations
* keep navigation depth to at most three levels
* add a minimal admin account edit page inside the new shell

#### PR-C — Global Context Switcher Foundation

* add one always-visible context switcher for store, language, and currency
* define request/context propagation so admin pages and APIs receive explicit scope rather than inferring it indirectly
* make scope visible in page headers and editing surfaces

#### PR-D — Scoped Data Model And API Contracts

* define which admin-managed fields are global, translatable, or store-specific
* add backend contracts for scoped reads and writes without introducing deep inheritance chains
* implement the simple override model: global values plus store-level overrides only
* align permission checks with scoped operations where needed

#### PR-E — Scoped Editing UX For Catalog And Content

* update product editing to show global versus store-scoped versus translatable fields explicitly
* update content editing to expose language and store assignment clearly
* ensure page-level UI makes overrides and scope obvious instead of hidden behind separate screens

#### PR-F — Feature-Local Configuration And Store Management Separation

* move operational settings closer to features such as payments, shipping, and tax
* keep Store Management dedicated to stores, domains, languages, and currencies
* avoid a giant Settings tree by splitting structural concerns from behavioral feature config

#### PR-G — Admin Roles, Context-Aware APIs, And Audit Trail

* finish the permission model so it aligns with admin domains and scoped actions
* expose context-aware admin APIs consistently across the refactored sections
* add audit logging for scoped changes where admin state can differ by store or translation context

### Validation Target

When Track 3 ships, an operator should be able to log into `/admin` without exposed default credentials, load the product grid successfully after seeding, call the relevant admin product endpoints without erroneous 403 responses, edit their own admin account details, and understand the display format setting without external documentation.

The longer-term Track 3 end state should also provide one domain-driven admin shell with visible store/language/currency context, explicit scoped editing, feature-local configuration, and no hidden fallback model.

---

## Track 4 — Customer Account UX And Account-Security Hardening

### Goal

Make storefront account state visible and navigable, add complete customer self-service account management, and protect sensitive account changes with stronger authentication.

### Reported Issues To Track

* there is no obvious account icon or account link near the minicart, so a shopper cannot tell whether they are logged in, as which account, or how to log out quickly
* the account area currently shows orders only; it needs a full customer self-service form for editing customer details and changing password
* account and profile likely need to be split into separate pages, with account focused on credentials/basic identity data and profile focused on addresses and preferences
* email 2FA should be added during account creation, and accessing account-edit or other secrets/basic-user-data pages should require an additional login or step-up authentication page even if the shopper is already signed in; profile/preferences pages do not need the same gate

### Expected Scope

* add a storefront account entry point near the minicart that exposes logged-in state, current user context, and fast navigation to account actions including logout
* expand the customer account area beyond orders into editable account management flows
* separate sensitive account/security settings from profile, address, and preference management so the UI matches the data sensitivity boundary
* add email-based 2FA or verification flow during registration and a step-up authentication checkpoint before sensitive account-edit routes

### Design Constraints

* account state must be understandable from global storefront navigation without adding header clutter or conflicting with cart behavior
* sensitive account changes should require stronger proof than low-risk profile editing, but the flow must remain usable enough not to lock users out of normal account tasks
* the split between account and profile pages should follow data sensitivity and recovery risk, not arbitrary UI grouping
* logout, account discovery, and reauthentication flows should stay explicit and predictable across desktop and mobile layouts

### Open Questions

* should the extra authentication gate for account-edit pages reuse the existing password login, a one-time email code, or both depending on the session age and action sensitivity?
* is registration-time email 2FA a mandatory activation step before the account becomes usable, or a verification checkpoint that can be completed just before sensitive account access?
* which fields belong in `account` versus `profile`: name, phone, marketing preferences, default addresses, saved addresses, locale, and newsletter settings?

### Validation Target

When Track 4 ships, a shopper should be able to see from the header whether they are signed in, open clear account navigation, edit customer details and password through dedicated self-service pages, manage profile/preferences separately from secrets/basic account data, and complete stronger authentication before accessing sensitive account-edit routes.