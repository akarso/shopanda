# Merchant Guide

This guide is for store operators who manage products, orders, and day-to-day storefront operations after Shopanda has already been installed.

If you are responsible for installing or hosting the application itself, handle deployment and server setup before working through this guide.

## Getting Started

### Open the admin area

1. Open `/admin` in your browser.
2. Sign in with the seeded admin account:
   - email: `admin@example.com`
   - password: the value you set in `SHOPANDA_SEED_ADMIN_PASSWORD` before running `app setup` or `app seed`
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

## Manage Products

### Create a product

1. Open `/admin/products`.
2. Select `New Product`.
3. Fill in the product form fields shown on screen.
4. Save the product.

The product form is schema-driven, so the exact fields can vary by deployment. Use the fields your store exposes rather than assuming every catalog uses the same product structure.

### Add or update variants

Variants are managed on the product edit page.

1. Open an existing product.
2. Scroll to the `Variants` section.
3. Add a new variant with SKU, name, and weight.
4. Update existing variant rows as needed.

Use variants for sellable options such as size, pack size, or material when each option needs its own SKU or stock tracking.

### Set prices

The current embedded admin UI focuses on product records, variants, and media. Bulk price changes are available through CSV tools.

If you or your operator has shell access, use:

```bash
app export:prices prices.csv
app import:prices prices.csv
```

Use price export before large edits so you have a clean rollback file.

### Upload images and assign a featured image

1. Open `/admin/media`.
2. Upload images with the file picker or drag-and-drop.
3. Return to a product edit page.
4. In `Featured Image`, choose an asset from the media library.

The media library shows a thumbnail, file name, file size, public URL, and delete action for each asset.

### Organize products into categories

Category structure exists in the platform and can be maintained through data import/export tooling.

If your product form exposes category-related fields, assign products there. For larger category changes, use CSV workflows:

```bash
app export:categories categories.csv
app import:categories categories.csv
```

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

### Update order status

The current admin flow supports the following progression:

- `pending` -> `confirmed`
- `confirmed` -> `paid`
- `pending` or `confirmed` -> `cancelled`
- `pending` -> `failed`

Use these transitions consistently:

- move to `confirmed` when the order is accepted for fulfillment
- move to `paid` when payment is settled
- move to `cancelled` when the order should not be fulfilled
- move to `failed` when checkout or payment did not complete successfully

### Issue refunds

Refunds are only available when the active payment provider supports them, such as Stripe.

In the current release, refund handling is wired through the admin order refund endpoint rather than a dedicated embedded admin page. If your store uses Stripe refunds, confirm with your technical operator how refunds are exposed in your deployment.

### View invoices

Shopanda generates invoice emails with an attached PDF when invoice creation is wired in the deployment.

In the current release, invoice delivery is email-first:

- customers receive the invoice PDF as an attachment
- operators should treat the email record as the primary invoice handoff
- the embedded admin SPA does not yet include a dedicated invoice viewer

## Configure the Store

### Update store settings

Open `/admin/settings` to manage the grouped settings page.

Available sections:

- `Store Info`
- `Email`
- `Media`
- `Currency`
- `Tax`

Save each section independently.

### Store Info

Use the `Store Info` section for:

- store code
- store name
- domain
- country
- language
- currency
- default-store flag
- presentation fields such as address and logo URL

Current behavior uses the default store, or the first available store when no default exists.

### Email settings

Use the `Email` section to configure SMTP delivery:

- SMTP host
- port
- username
- password
- sender address

After saving, send a test email from the settings page to verify delivery before relying on order or password-reset emails.

### Media settings

Use the `Media` section to manage storage-related values such as local or S3-backed media configuration, depending on what your deployment supports.

### Currency and tax settings

Use the `Currency` section for display defaults and the `Tax` section for tax behavior such as the default tax class and whether pricing is tax-inclusive.

### Shipping zones and rates

Shipping zones, countries, rate tiers, weight-based rules, and free-shipping thresholds are implemented in the platform, but they are currently managed through admin APIs rather than the embedded settings page.

If your team manages shipping directly, the technical interface is under:

- `GET /api/v1/admin/shipping/zones`
- `POST /api/v1/admin/shipping/zones`
- `PUT /api/v1/admin/shipping/zones/{id}`
- `GET /api/v1/admin/shipping/zones/{id}/rates`

Many merchants handle this once during setup, then revisit it only when rates or regions change.

### Payment provider setup

Payment provider selection and credentials are deployment-level concerns in the current release. Merchants usually coordinate these with the person who manages application configuration.

Operationally, the most visible merchant effect is that some features, such as automated refunds, only appear when the active provider supports them.

### Multi-store usage

Shopanda includes store entities, but the current embedded admin settings page is optimized around the primary store selection rather than a full multi-store operator workflow. If you run multiple stores, confirm with your technical operator which store is currently marked as default before editing shared settings.

## Day-to-Day Operations

### Suggested daily routine

1. Open the dashboard and scan low stock and recent orders.
2. Review new `pending` orders.
3. Confirm or cancel orders that need action.
4. Check product availability and update stock through your normal import/export process when needed.
5. Verify email settings after any infrastructure or credential changes.

### Process orders consistently

Use a predictable workflow for every order:

1. Review the order detail.
2. Confirm that payment and order contents look correct.
3. Move the order to `confirmed`.
4. Mark it `paid` once settlement is complete.
5. Fulfill outside Shopanda if your warehouse flow is external.

### Watch low stock

The dashboard exposes a low stock count so operators can spot replenishment issues quickly. Use stock CSV export/import when many variants need updates at once.

### Handle customer inquiries

Use the order detail page as the source of truth for:

- order ID
- current status
- item list
- totals
- customer reference

This is usually enough to answer “where is my order?” and “what did I buy?” style support questions, even when fulfillment happens in another tool.

### Understand email notifications

Shopanda supports operational emails such as:

- order confirmation
- password reset
- invoice email with PDF attachment

If customers report missing emails:

1. verify SMTP settings in `/admin/settings`
2. send a test email
3. confirm the sender address and mail credentials are still valid

## Current Release Notes

The merchant-facing experience is usable today, but a few workflows still live outside the embedded admin SPA:

- bulk catalog changes use CLI CSV tools
- shipping zone management is API-backed
- payment-provider setup is deployment-level configuration
- invoice viewing is email-first rather than an in-app viewer
- refunds depend on provider support and deployment wiring

That split is intentional for now: the admin SPA covers the most frequent merchant workflows first, while more technical or less frequent operations remain explicit and scriptable.