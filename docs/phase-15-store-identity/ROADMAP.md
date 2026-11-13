# Phase 15 — Store identity & appearance roadmap

## Strategy

* Merchants operate identity, branding, and transactional mail from admin — the same bar Phase 6 set for nav/blocks/webhooks.
* Domain first (Track A), then consumers (invoice, email layout, storefront). Appearance and email-template admin can start in parallel once identity is readable.
* Prefer vertical slices: domain → postgres → admin API → admin UI → one consumer.
* PR specs live under `prs/` (**PR-1200–1240**). Each PR is **`[oss]`**.
* Individual PR specs at this stage carry Summary / Why / Scope / Out of scope / Follow-up only. Validation and documentation sections are written when the PR is implemented.

## Tracks

| Track | Goal | PR range | Delivers |
| --- | --- | --- | --- |
| **A** | Store identity | PR-1200–1205 | Legal name, contact emails, tax-ID registry, four address roles, invoice/email/storefront consumers |
| **B** | Transactional email templates | PR-1210–1214 | File loader wired, remaining templates on disk, DB override, admin editor + preview + test send |
| **C** | Appearance | PR-1220–1223 | Theme discovery + activate, Site Identity (logo upload, title, tagline, favicon), default theme reads it |
| **D** | Legal CMS + footer | PR-1230–1231 | Seeded About / Privacy / Terms / Imprint; footer uses CMS + identity |
| **E** | Docs closeout | PR-1240 | Specs, C4, MERCHANT / RUNBOOK / config example |

Recommended order: **A (1200–1202)** first — everything else reads identity — then **A UI (1203–1204)** and **B 1210** / **C 1220** in parallel. **A 1205** after 1202. **D** after A UI + C theme consume. **E** last.

```text
1200 → 1201 → 1202 → 1203 → 1204
                 │
                 └──> 1205 (invoice PDF + EmailData + storefront/public identity)

1210 → 1211 → 1212 → 1213 → 1214     (Track B; 1210 can start immediately)
1220 → 1221 → 1222 → 1223           (Track C; 1223 wants 1222)
1230 → 1231                         (Track D; wants 1202 + 1223)
                                         └──> 1240
```

## Target outcome

When Phase 15 ships, a merchant **without shell access** should be able to:

- Fill **Store Information**: legal name, phone, hours, general/sales/support emails
- Maintain **four structured addresses** (general, invoice, shipping origin, returns)
- Add **tax IDs** that match the store country (NIP, EU VAT, …) plus extra types
- See that identity on **invoice PDFs**, **email footers**, and the **storefront footer**
- Open **Appearance → Themes**, activate a discovered theme
- Upload a **logo** (and favicon), set **site title** and **tagline**, and see them on the default theme
- Edit **transactional email** subject/body from admin, preview, send a test
- Have **About / Privacy / Terms / Imprint** pages linked from the footer (seeded, then edited as CMS)

## Track A — Store identity (PR-1200–1205)

**Goal:** Magento-like Store Information + store addresses. Country-specific IDs without country-specific schema.

See [`specs/STORE_IDENTITY.md`](specs/STORE_IDENTITY.md).

| PR | Title | Short description |
| --- | --- | --- |
| PR-1200 | Identity domain | `Identity` (legal name, phone, hours, contact emails), `Address` + four roles, `TaxIdentifier`. No postgres yet. |
| PR-1201 | Tax identifier type registry | Core catalog (`pl_nip`, `eu_vat`, `de_ustidnr`, `us_ein`, …) + `RegisterTaxIdentifierType` plugin port. |
| PR-1202 | Persistence + admin/public API | Migration; store-scoped CRUD; backfill `store.address` into `general`; `GET` public identity for storefront/headless. |
| PR-1203 | Store Information admin page | Settings → Store Information: legal name, phone, hours, three emails. Move those fields off Settings → General. |
| PR-1204 | Address pages | Settings → Store Addresses: index of four roles + per-role structured form (Magento-like). |
| PR-1205 | Consumers | Invoice PDF seller block; `EmailData` from identity; storefront layout + footer partial fields. |

**Out of scope for the track:** Peppol e-invoicing, per-country checksum validation (format hints only), GPSR auto-fill from legal name, Magento “sender identity” picker per email type.

## Track B — Transactional email templates (PR-1210–1214)

**Goal:** Honor the Phase 2 resolution order and give merchants an editor.

See [`specs/EMAIL_TEMPLATES_ADMIN.md`](specs/EMAIL_TEMPLATES_ADMIN.md) and [Phase 2 EMAIL_TEMPLATES](../phase-2-merchant-ready/specs/EMAIL_TEMPLATES.md).

| PR | Title | Short description |
| --- | --- | --- |
| PR-1210 | Wire file loader | `email.LoadDir` in `cmd/`; wrap with `_layout.html`; fill `EmailData` from current store name/address/logo (identity in 1205). |
| PR-1211 | Remaining file templates | `email_verification`, `security_verification`, `email_change_*`, `cart_recovery`, guest order-claim — same layout. |
| PR-1212 | DB template store | Table + loader: DB (name + locale) → file → builtin. |
| PR-1213 | Admin email-template API | List registered names; GET/PUT subject+html per locale; reset-to-file. |
| PR-1214 | Admin editor UI | Content → Email Templates: list, edit, preview (sample data), test send via existing SMTP test path. |

**Out of scope for the track:** WYSIWYG / drag-drop, A/B tests, marketing/newsletters, MJML, SMS/push, per-email “from name” picker.

## Track C — Appearance (PR-1220–1223)

**Goal:** WordPress-like Themes page + Site Identity. Not a live customizer.

See [`specs/APPEARANCE.md`](specs/APPEARANCE.md).

| PR | Title | Short description |
| --- | --- | --- |
| PR-1220 | Theme catalog + activate API | Scan `themes/*/theme.yaml`; store-scoped active theme; `frontend.theme_path` is fallback. |
| PR-1221 | Admin Themes page | Appearance → Themes: cards (name, parent, screenshot), Activate. |
| PR-1222 | Site Identity | Appearance → Site Identity: title, tagline, logo + favicon via media library (replaces `store.logo` URL field). |
| PR-1223 | Default theme consume | `_header.html` / `_footer.html` / `_starter` use logo, title, tagline; drop hardcoded “Merchant-ready storefront”. |

**Out of scope for the track:** Live preview iframe, theme upload ZIP, SCSS pipeline, color palettes, Magento layout XML, embedding themes in the binary.

## Track D — Legal CMS + footer (PR-1230–1231)

**Goal:** Magento-like CMS legal pages that the footer actually links.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1230 | Seed legal pages + footer menu | About, Privacy, Terms, Imprint (store language); footer menu items `page` type. Idempotent seed. |
| PR-1231 | Footer identity + CMS links | Default footer renders DB footer menu and identity (legal name, general address, tax IDs). Remove hardcoded Legal/Privacy spans. |

**Out of scope for the track:** Contact form, CMS `{{identity.*}}` tokens, per-country legal copy packs, cookie consent.

## Track E — Docs closeout (PR-1240)

| PR | Title | Short description |
| --- | --- | --- |
| PR-1240 | Spec + operator docs | Supersede THEME_SYSTEM / EMAIL_TEMPLATES non-goals; C4 components; MERCHANT.md, RUNBOOK.md, `config.example.yaml`, PLUGINS.md registry note. |

## Out of scope (phase)

| Item | Why |
| --- | --- |
| Magento layout XML / block reorder | Phase 7 / 14 non-goal; slots stay |
| WordPress Customizer live preview | Admin form + storefront refresh is enough |
| Theme ZIP upload / marketplace | Operators still drop a folder on disk |
| Country plugin packs / checksums | Registry + format hints; checksums later |
| Peppol / e-invoicing | Phase 6 backlog, still later |
| Marketing email / newsletter | Transactional only |
| GPSR / WEEE / OSS settings move | Stay on Operations → Shipping |
| Official React storefront | Unchanged |

## Licensing

All PRs **`[oss]`** (GPL v3). No B2B dependency.
