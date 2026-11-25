# Phase 18 — Vouchers

## Intent

When a virtual product with voucher settings is paid, Shopanda issues one or more codes, renders a PDF, and emails it. Staff look the code up and mark it redeemed. The register shows code, order, status, who redeemed it, and when.

A voucher is not spent at checkout. It is not a discount.

## Locked decisions

- Shell: **virtual** products with `voucher_settings.enabled`
- Codes: `codes_per_unit` × line quantity
- Redemptions: `max_redemptions` per code (default 1)
- Auth: `vouchers.read` / `vouchers.write` (Admin, Manager, Support)
- Undo: staff unredeem with audit

## Non-goals

- Coupon-style cart discounts
- Store-credit top-up codes
- Seat maps / external box-office replacement
- Theatre-site implementation (shop issues and redeems)

## PR index

| PR | Responsibility | Status |
| --- | --- | --- |
| [PR-1800](prs/PR-1800.md) | Domain + migration + postgres | done |
| [PR-1801](prs/PR-1801.md) | Settings + product admin API | done |
| [PR-1802](prs/PR-1802.md) | order.paid issuer | done |
| [PR-1803](prs/PR-1803.md) | Admin register API + RBAC | done |
| [PR-1804](prs/PR-1804.md) | Redeem + unredeem | done |
| [PR-1805](prs/PR-1805.md) | PDF + email | done |
| [PR-1806](prs/PR-1806.md) | Admin GUI | done |
