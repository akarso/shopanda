# Email templates (admin) — specification

Completes [Phase 2 EMAIL_TEMPLATES](../../phase-2-merchant-ready/specs/EMAIL_TEMPLATES.md) and [Phase 1 EMAILS_NOTIFICATIONS](../../phase-1-core/specs/EMAILS_NOTIFICATIONS.md) §6.

Those specs already defined file templates, `_layout.html`, `EmailData`, and resolution **DB → file → builtin**. PR-1210 wired `LoadDir`; PR-1212 adds DB overrides at render time. Admin editor remains PR-1213–1214.

## 1. Wiring (PR-1210)

On boot:

1. Register builtin fallbacks (`RegisterTemplates`) — unchanged.
2. `LoadDir("templates/emails")` overwrites builtins when files exist.
3. After PR-1212, wrap the loader with a DB source first.

Fill `EmailData` on every send path:

| Field | Source (after Track A 1205) | Until 1205 |
| --- | --- | --- |
| `StoreName` | `store.Name` | `store.Name` (cart recovery uses identity brand; no `"Shopanda"` hardcode) |
| `StoreURL` | store domain | as today |
| `LogoURL` | Site identity logo, else `store.logo` | `store.logo` |
| `StoreAddress` | formatted `general` address | `store.address` |

Guest order-claim mail in `cmd/api` uses the named `order_claim` template (PR-1211).

## 2. File set

On disk (PR-1210 + PR-1211): `_layout.html`, `order_confirmed`, `order_shipped`, `password_reset`, `invoice_created`, plus:

* `email_verification.html`
* `security_verification.html`
* `email_change_verification.html`
* `email_change_notice.html`
* `cart_recovery.html`
* `order_claim.html` (guest claim; `cmd/api` claim emailer renders this name)

`voucher_issued` remains builtin-only for now.

## 3. DB override (PR-1212)

```text
email_templates
  id, store_id, name, locale, subject, body, updated_at, updated_by
  UNIQUE (store_id, name, locale)
```

`locale` empty = store default language override row. Locale keys are the BCP 47 primary subtag (lower case); `Validate` / `Get` / `Delete` canonicalize (`DE` / `pt-BR` → `de` / `pt`). A non-empty input that does not yield a primary subtag (`und`, `deutsch`) is rejected — it must not alias onto the empty-locale default row. Render-only: an unknown request language still falls through to the empty row via `overrideLocales`. Senders never invent synthetic `en` for template lookup.

Render-time lookup (`RenderWithOverrides`):

1. Exact requested locale (when non-empty)
2. Empty locale (store default language row)
3. In-memory registry (file `LoadDir` over builtin `RegisterTemplates`)

Only overridable names may be stored or applied from DB: `order_confirmed`, `order_shipped`, `invoice_created`, `cart_recovery`. Auth/token, claim, and voucher templates (`password_reset`, verification, email-change, `order_claim`, `voucher_issued`) stay file/builtin so a DB row cannot rewrite live token URLs, claim links, or voucher codes (contains-checks cannot stop an image beacon or rewritten `href`).

Subject uses `text/template` (SMTP header stays plain: `Fish & Chips`); executed subjects that contain CR/LF are rejected before enqueue. Body uses `html/template`. Layout still wraps body HTML. No MJML. Boot still registers builtins then files; DB is consulted only when rendering.

When overrides are wired, a default-store lookup failure aborts **overridable** sends only (so a stock file body is never acknowledged for those names). Auth/token, claim, and voucher mail still render from the file registry on a stores-table error (`BrandEmailData` and claim `resolve` are best-effort). Recovery does not mark the cart sent on store/locale lookup failure. The `order_claim` body always comes from file/builtin.

## 4. Admin API (PR-1213)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/admin/email-templates` | Registered names + source (`builtin` / `file` / `db`) + locales present (metadata only; no subject/body) |
| GET | `/api/v1/admin/email-templates/{name}` | Exact locale subject + body (query `locale`); no empty-locale fallthrough |
| PUT | `/api/v1/admin/email-templates/{name}` | Save DB override (parse + sample execute before Upsert) |
| DELETE | `/api/v1/admin/email-templates/{name}` | Remove override (allowlist only; revert to file/builtin) |
| POST | `/api/v1/admin/email-templates/{name}/preview` | Render with sample payload (no send); reports source / resolved_locale / fallback |
| POST | `/api/v1/admin/email-templates/{name}/test-send` | Overridable names only; `[TEST]` subject; mailbox check; short Send deadline |

RBAC: `content.read` (list/get/preview), `content.write` (put/delete/test-send). Store scope is the default store.

**PUT:** allowlist-only. Before Upsert, subject/body are parsed and executed with the same sample data preview uses; executed subjects containing CR/LF are rejected. A failed validation leaves no row.

**GET vs preview:** GET is exact-locale (DB row for that key, else file/builtin source text). Preview and test-send use the live render chain (requested locale → empty locale → file) unless `exact=1` / `exact=true`. Responses include `source`, `resolved_locale`, and `fallback` so a default-locale DB override is visible when previewing `de`.

**List `source`:** `db` when any allowlisted locale row exists for that name (including `de`-only). Locales are sorted by name/locale; empty locale is JSON `""`.

**Test-send:** overridable names only (not auth/claim/voucher). `to` must parse as a mailbox; subject is prefixed with `[TEST] `; Send runs under a ~15s deadline with a light per-admin rate limit. Invoice/voucher previews set `attachment_omitted` (no PDF is attached).

**Preview body:** empty body is OK; non-empty body uses strict JSON decode (422 on error). Sample maps are per-template (`cart_recovery` uses `Quantity` / `UnitPrice` / `LineTotal`).

`updated_by` is the admin actor id (required on write).

## 5. Admin UI (PR-1214)

**Content → Email Templates** (`/admin/content/email-templates`).

* List: name, description, current source, updated
* Edit: subject + HTML textarea (not a visual builder)
* Preview pane (server-rendered sample)
* Test send (to field)
* Revert to default

Abandoned-cart settings keep enable/delay; they link here instead of a dead “template pick” (PR-641 out of scope).

## 6. Non-goals

* WYSIWYG / drag-drop
* A/B or marketing campaigns
* Per-template From / Reply-To picker (use identity fallbacks)
* SMS / push
* Translating via the product-translation UI (locale column on this table is enough)
