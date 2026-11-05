# Phase 15 — Store identity & appearance

Give a merchant Magento-like store information (legal identity, addresses, tax IDs, contact emails) and a WordPress-like Appearance area (activate a theme, upload a logo, set title/tagline) plus an admin editor for transactional email templates. No SSH, no YAML, no theme fork for a logo.

## Why this phase

Phase 6 shipped a working admin. Phase 14 finished theme packaging (`theme:init`, inheritance, headless). A merchant still cannot run a real shop from admin:

| Gap | Today | Impact |
| --- | --- | --- |
| Store address | One free-text `store.address` textarea | Cannot print a legal invoice or footer |
| Tax IDs (VAT, NIP, EIN, …) | Missing | Illegal invoices / imprint in most EU countries |
| Contact emails | One SMTP From | No sales / support / general identities |
| Phone / legal company name | Missing | GPSR manufacturer name is product-safety, not seller identity |
| Invoice PDF seller block | Missing | PDF has invoice # and customer id only |
| Transactional email templates | Hardcoded strings in `RegisterTemplates` | File templates exist but are not loaded; no admin edit |
| Theme switch | `frontend.theme_path` / env / CLI | No `/admin/themes` |
| Logo / tagline | Logo is a URL string unused by the default theme; tagline is hardcoded | Appearance requires a theme fork |

This phase closes that merchant-presence gap. It does **not** add Magento layout XML, a WordPress customizer iframe, or a country-specific plugin pack.

## Design principles

- **Registry, not country columns.** Tax IDs are `{type, value}` rows. Core ships a catalog of types (NIP, EU VAT, EIN, …) with country applicability. Plugins register more. Admin shows types for the store country plus “add other.”
- **Address roles, not one blob.** Four Magento-like roles: `general`, `invoice`, `shipping_origin`, `returns`. Structured fields (line, city, region, postcode, country).
- **Identity ≠ branding.** Store Information is legal/contact. Appearance → Site Identity is title, tagline, logo, favicon.
- **Filesystem stays the theme source.** Admin activates a discovered directory. `frontend.theme_path` remains the deploy fallback.
- **Email resolution stays `DB → file → builtin`.** Phase 2 already specified this; production never wired files or DB. This phase does.
- **Reuse media, CMS, and SMTP.** Logo upload goes through the existing media library. Legal pages are seeded CMS pages. SMTP From stays the envelope sender.
- **One PR = one responsibility**, reviewable in ~10–20 minutes.

See [`ROADMAP.md`](ROADMAP.md), [`specs/`](specs/), and [`prs/`](prs/).

## Status

**Planned.** PR range **PR-1200–1240**. Independent of Phase 12 catalog work and Phase 13 plugin runtime; can run in parallel. Do not start a merchant site without Track A (identity on invoices) at minimum.

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 2 | File email templates + `EmailData` (loader unused in `cmd/`) | Shipped (incomplete wiring) |
| Phase 5 | EU compliance flags (Omnibus / WEEE / EPR / GPSR / OSS) | Shipped — stays separate from seller tax IDs |
| Phase 6 | Merchant-complete admin; email editor left as backlog | Shipped |
| Phase 7 Track F / 14 | Theme inheritance, slots, `theme:init`, headless | Shipped — no activate UI |
| **Phase 15** | Store identity, email template admin, Appearance | **Planned** |
