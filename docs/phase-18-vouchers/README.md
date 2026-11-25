# Phase 18 — Vouchers

A paid order can issue a code the buyer receives as a PDF. Staff redeem that code later (box office, till), and the admin keeps the register.

## Why this is not a coupon or store credit

| Thing that exists | What it does | Why it is not a voucher |
| --- | --- | --- |
| Coupon (`internal/domain/promotion`) | Code activates a promotion on a cart | Discounts checkout. Nothing to hand to a cashier |
| Store credit (PR-505) | Customer balance, redeemed at checkout | Money on an account. No code, no PDF, no staff redemption |

## Status

**Done** (PR-1800–1806), including CR hardening: void-on-return, CorePermissions, authenticated PDF GUI, snapshot entitlement, SQL status filter, publish-after-commit, email id-set dedupe, guest `customer_id` link, admin reissue.

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 5 PR-505 | Store credit balance | Shipped — explicitly left gift-card codes out |
| **Phase 18** | Issued code, PDF, staff redemption register | Active |
