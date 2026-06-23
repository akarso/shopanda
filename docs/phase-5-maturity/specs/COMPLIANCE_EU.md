# EU & Compliance Reference (Phase 5)

This document maps **common EU merchant obligations** to Shopanda's current capabilities and planned Phase 5 work. It is **not legal advice** — merchants remain responsible for jurisdiction-specific compliance.

---

## Summary Matrix

| Directive / regulation | What merchants need | Shopanda today | Phase 5 target |
| --- | --- | --- | --- |
| **GDPR** | Lawful processing, access, erasure, consent | Cookie consent, `GET/DELETE /account`, data export | Hardening only if gaps found |
| **Omnibus (Price Indication)** | Show lowest price in prior 30 days when showing a discount | `price_history` table, `PriceIndicationStep` in composition pipeline | PR-530: verify storefront display + admin toggle |
| **WEEE** | Register as producer; display symbol + reg. number; take-back info | Not modeled | PR-531: product/store compliance fields + PDP templates |
| **EPR (packaging)** | Report packaging placed on market; registration per member state | Not modeled | PR-532: packaging metadata on products/variants |
| **GPSR** (Dec 2024) | Manufacturer/importer details, warnings, traceability | Not modeled | PR-533: safety metadata + PDP disclosure |
| **VAT / OSS-IOSS** | Correct VAT on cross-border B2C; periodic OSS returns | Country tax rates, store scoping | PR-534 (stretch): export helpers, not filing automation |
| **E-invoicing (Peppol, etc.)** | Structured invoices in B2G/B2B markets | PDF invoices + credit notes | PR-534 (stretch): adapter core plugin |
| **Consumer Rights Directive** | Withdrawal period, clear pre-contract info | Order emails, CMS pages (merchant-authored) | Returns workflow (PR-502–503) supports withdrawal handling |
| **DSA** (marketplaces) | Trader traceability, complaint handling | N/A for single-merchant stores | Marketplace mode explicitly out of scope |

---

## Omnibus Directive (Price Indication)

**Requirement (simplified):** When showing a price reduction, disclose the **lowest price** applied in the **30 days** before the reduction.

**Current implementation:**

- `price_history` records snapshots on price changes
- `pricing.PriceHistoryRepository.LowestSince` queries the minimum in a window
- `composition.PriceIndicationStep` enriches PDP/PLP responses with lowest-prior price data

**Phase 5 (PR-530):**

- Confirm SSR templates render the indication block on discounted products
- Store-level enable/disable and styling hooks
- Admin documentation for merchants (when discounts trigger recording)

---

## WEEE Directive

**Requirement (simplified):** Producers of electrical/electronic equipment must register, mark products, and provide take-back/recycling information.

**Typical product data:**

| Field | Purpose |
| --- | --- |
| WEEE category | e.g. large household, small IT |
| Producer registration number | Per-member-state registry ID |
| Take-back instructions | Customer-facing recycling guidance |
| WEEE symbol visibility | On PDP/packaging info |

**Phase 5 (PR-531):**

- Optional product attributes (or dedicated compliance group) for WEEE fields
- Theme helper partial for symbol + registration line
- No automatic filing to national registries (merchant/export plugin territory)

---

## EPR — Packaging

**Requirement (simplified):** Report quantities of packaging materials placed on market; registration with national schemes.

**Typical data:**

- Material type and weight per SKU or shipment
- Recyclability / recycled content flags
- Scheme membership IDs per country

**Phase 5 (PR-532):**

- Structured packaging metadata on product/variant
- CSV export for merchant EPR reporting tools
- Keep calculation/reporting external — core stores facts

---

## GPSR (General Product Safety Regulation)

**Requirement (simplified):** Products sold in the EU need traceable manufacturer/importer contact, identifiers, and safety information where applicable.

**Typical data:**

- Manufacturer name and EU contact
- Product identifier (GTIN, SKU linkage)
- Warnings, age restrictions, CE/UKCA references where relevant

**Phase 5 (PR-533):**

- Compliance attribute group on catalog
- PDP section rendered when fields present
- Admin validation for required combinations (category-driven rules later as plugin)

---

## GDPR (baseline — already shipped)

Implemented in PR-098 and account flows:

- Consent preferences (`analytics`, `marketing`)
- Account data export and deletion
- Cascade delete on customer removal

Phase 5 may add audit-log correlation for admin-initiated customer deletes (already partially logged).

---

## Cross-Border VAT (OSS / IOSS)

Shopanda supports **country-based tax rates** and store scoping. Merchants still use external accounting for OSS return filing.

**Phase 5 stretch (PR-534):**

- Export order tax breakdown by destination country
- Optional OSS summary report (CSV)
- E-invoice generation via Peppol as **core plugin**, not mandatory core

---

## Implementation Principles

1. **Facts in core, filing outside** — Store compliance metadata; do not pretend to submit government returns.
2. **Opt-in per catalog** — Electronics seller enables WEEE fields; fashion seller ignores them.
3. **Storefront disclosure via themes** — Templates/partials render when data exists; no hard-coded country branches in domain.
4. **Plugins for national variants** — Country-specific report formats belong in external or core plugins.
5. **Test with fixtures** — Each compliance PR includes PDP/order fixture tests for disclosure presence/absence.

---

## Related Docs

- [Phase 5 Roadmap](../ROADMAP.md)
- [Phase 1 — GDPR / consent PR](../phase-1-core/prs/PR-098.md)
- [Pricing pipeline — price history](../phase-1-core/specs/PRICING_PIPELINE.md)
- [Merchant Guide — current limitations](../../guides/MERCHANT.md#roadmap-and-known-gaps)
