# Merchant Guide

This guide is for store operators who manage products, orders, and day-to-day storefront operations after Shopanda has already been installed.

If you are responsible for installing or hosting the application itself, handle deployment and server setup before working through this guide.

## Getting Started

### Open the admin area

1. Open `/admin` in your browser.
2. Sign in with the seeded admin account:
   - email: `admin@example.com`
   - password: the value you set in `SHOPANDA_SEED_ADMIN_PASSWORD` before running `app setup` or `app seed`, if you chose to seed the default admin user
3. After login, Shopanda opens the dashboard.

If login fails, confirm that:

- the application has been migrated and seeded
- the admin user was created during seeding
- the password in your environment matches the one used when the seed ran

### Understand the dashboard

The dashboard at `/admin/dashboard` gives a quick operational snapshot:

- orders placed today
- revenue today
- total products
- low stock count
- recent orders with status and date

Use it as a daily start page. If low stock rises or recent orders begin to fail or stall, move straight into the Products or Orders sections.

### Admin map

Every major screen in the embedded admin SPA is listed below. Use the **Route** column to jump directly when you know the URL.

| Area | Screen | Route | Notes |
| --- | --- | --- | --- |
| — | Dashboard | `/admin/dashboard` | Daily snapshot |
| Sales | Orders | `/admin/orders` | List, detail, status updates |
| Sales | Returns | `/admin/sales/returns` | RMA workflow ([PR-502](../phase-5-maturity/prs/PR-502.md)) |
| Sales | Transactions | `/admin/sales/transactions` | Payment ledger ([PR-504](../phase-5-maturity/prs/PR-504.md)) |
| Catalog | Products | `/admin/products` | Create, edit, variants, media |
| Catalog | Categories | `/admin/catalog/categories` | Tree + product assignment ([PR-631](../phase-6-merchant-complete/prs/PR-631.md)) |
| Catalog | Attributes | `/admin/catalog/attributes` | Attribute groups + product fields |
| Catalog | Bulk Prices | `/admin/catalog/prices` | Grid price edits ([PR-630](../phase-6-merchant-complete/prs/PR-630.md)) |
| Catalog | Reviews | `/admin/catalog/reviews` | Moderation ([PR-640](../phase-6-merchant-complete/prs/PR-640.md)) |
| Customers | Customers | `/admin/customers` | Profile + store credit panel |
| Customers | Groups | `/admin/customers/groups` | **B2B license required** ([PR-610](../phase-6-merchant-complete/prs/PR-610.md)) |
| Marketing | Promotions | `/admin/marketing/promotions` | Guided + advanced rules ([PR-642](../phase-6-merchant-complete/prs/PR-642.md)) |
| Marketing | Coupons | `/admin/marketing/coupons` | Coupon codes |
| Marketing | Abandoned Cart | `/admin/marketing/abandoned-cart` | Recovery email settings ([PR-641](../phase-6-merchant-complete/prs/PR-641.md)) |
| Content | Pages | `/admin/content/pages` | CMS pages |
| Content | Navigation | `/admin/content/navigation` | Menu builder ([PR-600](../phase-6-merchant-complete/prs/PR-600.md)) |
| Content | Blocks | `/admin/content/blocks` | Reusable blocks ([PR-601](../phase-6-merchant-complete/prs/PR-601.md)) |
| Content | Home Blocks | `/admin/content/home-blocks` | Homepage placements ([PR-602](../phase-6-merchant-complete/prs/PR-602.md)) — linked from Blocks grid |
| Content | Media | `/admin/media` | Asset library |
| Operations | Inventory | `/admin/operations/inventory` | Stock levels |
| Operations | Shipping | `/admin/operations/shipping` | Zones, rates, EU compliance toggles ([PR-524](../phase-5-maturity/prs/PR-524.md)) |
| Operations | Payments | `/admin/operations/payments` | Currency display defaults |
| Settings | General | `/admin/settings` | Store info, email, media |
| Settings | Localization | `/admin/settings/localization` | Currency + store languages |
| Settings | Users & Roles | `/admin/settings/users` | Admin users ([PR-520](../phase-5-maturity/prs/PR-520.md)) |
| Settings | Audit Log | `/admin/settings/audit` | Admin action history |
| Settings | Extension Fields | `/admin/settings/extension-fields` | Custom field definitions ([PR-866](../phase-9-merchant-discovery/prs/PR-866.md)); variant values on product edit ([PR-867](../phase-9-merchant-discovery/prs/PR-867.md)) |
| Store | Stores | `/admin/store` | Multi-store entities |
| Store | Domains | `/admin/store/domains` | Hostname mapping |
| Store | Languages | `/admin/store/languages` | Store language config |
| Store | Currencies | `/admin/store/currencies` | Store currency config |
| Integrations | Integrations | `/admin/integrations` | SMTP/media/plugin summary |
| Integrations | Webhooks | `/admin/integrations/webhooks` | Outbound endpoints ([PR-620](../phase-6-merchant-complete/prs/PR-620.md)) — linked from Integrations |
| Integrations | Inbound Idempotency | `/admin/integrations/idempotency` | ERP callback dedupe keys ([PR-864](../phase-9-merchant-discovery/prs/PR-864.md)) — linked from Integrations |
| Account | Account | `/admin/account` | Your admin profile (header link) |

Use the **Store / Language / Currency** switcher in the admin header when a screen is scope-sensitive (catalog, settings, bulk prices, store credit).

## Manage Products

### Create a product

1. Open `/admin/products`.
2. Select **New Product**.
3. Fill in the product form fields shown on screen.
4. Save the product.

The product form is schema-driven, so the exact fields can vary by deployment. Use the fields your store exposes rather than assuming every catalog uses the same product structure.

### Add or update variants

Variants are managed on the product edit page.

1. Open an existing product.
2. Scroll to the **Variants** section.
3. Add a new variant with SKU, name, and weight.
4. Update existing variant rows as needed.

Use variants for sellable options such as size, pack size, or material when each option needs its own SKU or stock tracking.

### Set prices

**In admin:** open **Catalog → Bulk Prices** at `/admin/catalog/prices` to search variants and edit prices in a grid for the active store scope.

**Bulk CSV (optional):** operators with shell access can still use CLI export/import for large migrations:

```bash
app export:prices prices.csv
app import:prices prices.csv
```

Use price export before large edits so you have a clean rollback file.

### Upload images and assign a featured image

1. Open `/admin/media`.
2. Upload images with the file picker or drag-and-drop.
3. Return to a product edit page.
4. In **Featured Image**, choose an asset from the media library.

The media library shows a thumbnail, file name, file size, public URL, and delete action for each asset.

### Organize products into categories

1. Open **Catalog → Categories** at `/admin/catalog/categories`.
2. Create or edit categories in the tree.
3. On a product edit page, use the **Categories** checkbox tree to assign the product.

For very large catalog migrations, CSV workflows remain available:

**On the storefront:** category pages and the product listing show clickable category facet chips when search returns facet counts. Shoppers can refine by category without leaving the catalog ([PR-900](../phase-9-merchant-discovery/prs/PR-900.md)). Attribute-based filters are planned separately.

```bash
app export:categories categories.csv
app import:categories categories.csv
```

### Moderate product reviews

1. Open **Catalog → Reviews** at `/admin/catalog/reviews`.
2. Filter by status (pending, approved, rejected).
3. Approve or reject pending reviews.

You can also open the **Reviews** panel on a product edit page for a product-specific summary and link to the filtered moderation list.

### Import and export catalog data

Shopanda includes CLI-based CSV workflows for bulk operations.

Common commands:

```bash
app import:products products.csv
app export:products products.csv
app import:stock stock.csv
app export:stock stock.csv
app import:prices prices.csv
app export:prices prices.csv
app import:categories categories.csv
app export:categories categories.csv
```

Use these when:

- migrating catalog data from another system
- updating many prices or stock levels at once
- reorganizing categories in bulk

If you are not the server operator, hand the CSV files to whoever manages the Shopanda host.

## Manage Orders

### Review incoming orders

1. Open `/admin/orders`.
2. Use the status filter to narrow the list.
3. Open an order to review details.

The order detail page shows:

- order ID
- order status
- customer reference
- date
- line items
- total amount
- derived payment status
- **Invoices** panel (when issued)
- **Refund** panel (when eligible)

### Update order status

The admin flow supports the following progression:

- `pending` → `confirmed`
- `confirmed` → `paid`
- `pending` or `confirmed` → `cancelled`
- `pending` → `failed`

Use these transitions consistently:

- move to `confirmed` when the order is accepted for fulfillment
- move to `paid` when payment is settled
- move to `cancelled` when the order should not be fulfilled
- move to `failed` when checkout or payment did not complete successfully

### Issue refunds

On the order detail page, open the **Refund** section when the payment provider supports online refunds (Stripe).

- Only **full** refunds of the captured Stripe amount are supported in admin.
- If the button is disabled, read the eligibility message (wrong provider, already refunded, missing permission, etc.).
- Manual refunds outside Stripe must be recorded in your payment provider.

### View and download invoices

On the order detail page, the **Invoices** panel lists issued invoices for that order.

- Select **Download PDF** to save the invoice file.
- Customers still receive invoice emails with PDF attachments when invoice creation is wired in your deployment.

## Sales Operations

### Returns (RMA)

1. Open **Sales → Returns** at `/admin/sales/returns`.
2. Open a return to review line items and status.
3. Approve, reject, or mark received/refunded according to your workflow.

### Payment transactions

Open **Sales → Transactions** at `/admin/sales/transactions` for a read-only ledger of payment events (useful for reconciliation and support).

## Customers

### Customer profiles

Open **Customers → Customers** at `/admin/customers` to browse accounts and open a profile.

### Store credit

On a customer detail page, use the **Store Credit** panel to view balance and issue credit in the selected currency ([PR-611](../phase-6-merchant-complete/prs/PR-611.md)).

Select a currency in the header switcher if the panel asks for one.

### Customer groups (B2B)

**Requires the B2B plugin and a valid license.** Enable `plugins.b2b` and set `plugins.b2b.license_key` in configuration. See `plugins/b2b/README.md`.

When licensed:

1. Open **Customers → Groups** at `/admin/customers/groups`.
2. Create groups and assign members.
3. Group-specific variant prices use the B2B group price API (documented in the B2B plugin README).

Without a license, the groups screen shows setup instructions instead of the grid.

## Marketing

### Promotions and coupons

- **Promotions** (`/admin/marketing/promotions`): create catalog or cart promotions using templates (percentage, tiered quantity, buy-X-get-Y, fixed cart discount) or the **Advanced JSON** tab for power users.
- **Coupons** (`/admin/marketing/coupons`): attach coupon codes to promotions.

### Abandoned cart recovery

Open **Marketing → Abandoned Cart** at `/admin/marketing/abandoned-cart` to enable or disable recovery emails and set the delay (hours) before the first reminder is sent. Configure SMTP under **Settings → General** so emails can be delivered.

## Content and Storefront

### Pages, navigation, and blocks

- **Pages** (`/admin/content/pages`): CMS pages for legal text, landing pages, etc.
- **Navigation** (`/admin/content/navigation`): edit header/footer menus and link items to categories or pages.
- **Blocks** (`/admin/content/blocks`): reusable content sections; use **Home page blocks** from the blocks grid to manage homepage placements.
- **Media** (`/admin/media`): shared asset library.

## Operations

### Inventory

Open **Operations → Inventory** at `/admin/operations/inventory` to view and adjust stock by variant SKU.

### Shipping and compliance

Open **Operations → Shipping** at `/admin/operations/shipping` to configure:

- shipping zones and rates
- tax defaults
- EU compliance toggles (Omnibus, WEEE, EPR, GPSR, OSS export helpers)

### Payments display

Open **Operations → Payments** at `/admin/operations/payments` for currency display format. Payment **provider credentials** (Stripe keys, etc.) remain deployment-level configuration—coordinate with your technical operator.

## Configure the Store

### General settings

Open **Settings → General** at `/admin/settings` for:

- **Store Info** — code, name, domain, country, language, currency, address, logo
- **Email** — SMTP host, port, credentials, sender; send a test email after changes
- **Media** — storage backend settings (local or S3, depending on deployment)

### Localization

Open **Settings → Localization** at `/admin/settings/localization` for currency display and store language management.

### Admin users and audit

- **Users & Roles** (`/admin/settings/users`): manage admin accounts and roles.
- **Audit Log** (`/admin/settings/audit`): review admin actions.
- **Extension Fields** (`/admin/settings/extension-fields`): define custom field schemas for products, variants, cart lines, and other entities ([PR-866](../phase-9-merchant-discovery/prs/PR-866.md)). Product-scoped values are edited in the product **Extensions** panel; variant-scoped values are edited per row in the **Variants** panel on product edit ([PR-867](../phase-9-merchant-discovery/prs/PR-867.md)).

### Multi-store usage

Use **Store Management → Stores** at `/admin/store` for store entities, plus **Domains**, **Languages**, and **Currencies** sub-screens. Confirm which store is default before editing scope-sensitive catalog or settings.

## Integrations

Open **Integrations** at `/admin/integrations` for a summary of email, media, and plugin configuration. Follow the **Webhooks** link to manage outbound webhook endpoints at `/admin/integrations/webhooks`. Use **Inbound Idempotency** at `/admin/integrations/idempotency` to inspect ERP callback dedupe keys and preview stored replay responses ([PR-864](../phase-9-merchant-discovery/prs/PR-864.md)).

## Day-to-Day Operations

### Suggested daily routine

1. Open the dashboard and scan low stock and recent orders.
2. Review new `pending` orders.
3. Confirm or cancel orders that need action.
4. Check **Operations → Inventory** or bulk stock CSV when many variants need updates.
5. Verify email settings after any infrastructure or credential changes.

### Process orders consistently

Use a predictable workflow for every order:

1. Review the order detail.
2. Confirm that payment and order contents look correct.
3. Move the order to `confirmed`.
4. Mark it `paid` once settlement is complete.
5. Download invoice PDFs or confirm email delivery when required.
6. Fulfill outside Shopanda if your warehouse flow is external.

### Watch low stock

The dashboard exposes a low stock count. Use **Operations → Inventory** for quick adjustments, or stock CSV export/import for bulk updates.

### Handle customer inquiries

Use the order detail page as the source of truth for:

- order ID
- current status
- item list
- totals
- customer reference
- invoice and refund status

### Understand email notifications

Shopanda supports operational emails such as:

- order confirmation
- password reset
- invoice email with PDF attachment
- abandoned cart recovery (when enabled under Marketing)

**Order emails are sent asynchronously.** Checkout completes in the web server, but delivery depends on a background **worker** process and valid SMTP settings. In Docker Compose deployments, the default stack includes a `worker` service; on bare metal, run `shopanda worker` as a separate service (see the [Deployment Guide](DEPLOYMENT.md)).

If customers report missing emails:

1. confirm the worker process is running
2. verify SMTP settings in **Settings → General**
3. send a test email
4. confirm the sender address and mail credentials are still valid

## Current Release Notes

The embedded admin SPA now covers the day-to-day merchant workflows listed in the [Admin map](#admin-map) above. A few items remain outside the SPA or need operator coordination:

| Workflow | Where to manage |
| --- | --- |
| Bulk catalog migration | Admin grids + optional CLI CSV tools |
| Payment provider secrets | Deployment config (not in admin UI) |
| B2B customer groups | Admin UI when B2B plugin is licensed |
| Group-specific B2B prices | B2B API (see plugin README) |

That split is intentional: frequent tasks live in admin; large migrations and provider secrets stay scriptable or deployment-level.

## Roadmap and Known Gaps

Phases 4–6 delivered merchant-complete admin coverage for catalog, sales, marketing, content, operations, and settings. See the [Phase 6 Roadmap](../phase-6-merchant-complete/ROADMAP.md) for the full plan.

| Capability | Status |
| --- | --- |
| Navigation / blocks / home placements | Shipped — [PR-600](../phase-6-merchant-complete/prs/PR-600.md)–[602](../phase-6-merchant-complete/prs/PR-602.md) |
| Returns / transactions / invoices / refunds | Shipped — Phase 5–6 sales PRs |
| Bulk prices + category picker | Shipped — [PR-630](../phase-6-merchant-complete/prs/PR-630.md), [PR-631](../phase-6-merchant-complete/prs/PR-631.md) |
| Reviews + abandoned cart + promotion helper | Shipped — [PR-640](../phase-6-merchant-complete/prs/PR-640.md)–[642](../phase-6-merchant-complete/prs/PR-642.md) |
| Customer groups | B2B plugin + license — [PR-610](../phase-6-merchant-complete/prs/PR-610.md) |
| Store credit | Customer detail panel — [PR-611](../phase-6-merchant-complete/prs/PR-611.md) |
| Webhooks | Integrations → Webhooks — [PR-620](../phase-6-merchant-complete/prs/PR-620.md) |
| Payment provider setup | Deployment-level (Stripe keys, etc.) |
| Non-English admin UI | Not available |

EU directive overview: [Compliance Reference](../phase-5-maturity/specs/COMPLIANCE_EU.md).
