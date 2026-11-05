# Email templates (admin) — specification

Completes [Phase 2 EMAIL_TEMPLATES](../../phase-2-merchant-ready/specs/EMAIL_TEMPLATES.md) and [Phase 1 EMAILS_NOTIFICATIONS](../../phase-1-core/specs/EMAILS_NOTIFICATIONS.md) §6.

Those specs already defined file templates, `_layout.html`, `EmailData`, and resolution **DB → file → builtin**. Production still uses only `notification.RegisterTemplates` hardcoded HTML. `email.LoadDir` is never called from `cmd/`. Admin editor was an explicit non-goal; Phase 6 left it as backlog. This phase implements it.

## 1. Wiring (PR-1210)

On boot:

1. Register builtin fallbacks (`RegisterTemplates`) — unchanged.
2. `LoadDir("templates/emails")` overwrites builtins when files exist.
3. After PR-1212, wrap the loader with a DB source first.

Fill `EmailData` on every send path:

| Field | Source (after Track A 1205) | Until 1205 |
| --- | --- | --- |
| `StoreName` | `store.Name` | `store.Name` (cart recovery today hardcodes `"Shopanda"` — fix here) |
| `StoreURL` | store domain | as today |
| `LogoURL` | Site identity logo, else `store.logo` | `store.logo` |
| `StoreAddress` | formatted `general` address | `store.address` |

Guest order-claim mail in `cmd/api` becomes a named template, not an ad-hoc `mail.Message`.

## 2. File set

Already on disk: `_layout.html`, `order_confirmed`, `order_shipped`, `password_reset`, `invoice_created`.

PR-1211 adds the rest of `RegisterTemplates` plus claim:

* `email_verification.html`
* `security_verification.html`
* `email_change_verification.html`
* `email_change_notice.html`
* `cart_recovery.html`
* `order_claim.html` (guest claim)

## 3. DB override (PR-1212)

```text
email_templates
  id, store_id, name, locale, subject, body, updated_at, updated_by
  UNIQUE (store_id, name, locale)
```

`locale` empty = store default language. Lookup: exact locale → store language → file → builtin.

Body is inner HTML (layout still wraps). No MJML.

## 4. Admin API (PR-1213)

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/admin/email-templates` | Registered names + source (`builtin` / `file` / `db`) + locales present |
| GET | `/api/v1/admin/email-templates/{name}` | Subject + body for locale (query `locale`) |
| PUT | `/api/v1/admin/email-templates/{name}` | Save DB override |
| DELETE | `/api/v1/admin/email-templates/{name}` | Remove override (revert to file/builtin) |
| POST | `/api/v1/admin/email-templates/{name}/preview` | Render with sample payload (no send) |
| POST | `/api/v1/admin/email-templates/{name}/test-send` | Render + send to an address via existing mailer |

RBAC: reuse config/store admin permission or a dedicated `email_templates.write`. Do not invent a second SMTP stack.

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
