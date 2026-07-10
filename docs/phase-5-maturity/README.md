# Phase 5 — Mature Commerce

**Status: complete** (PR-500–544)

Phase 5 closed operational, compliance, and platform gaps on top of the Phase 4 product-complete baseline.

## Roadmap

- [**ROADMAP.md**](ROADMAP.md) — tracks, PR index, validation targets
- [**PR specs**](prs/README.md) — per-PR implementation notes (PR-500–544)

## Specs

| Document | Topic |
| --- | --- |
| [COMPLIANCE_EU.md](specs/COMPLIANCE_EU.md) | Omnibus, WEEE, EPR, GPSR, OSS/IOSS |
| [DYNAMIC_PLUGIN_LOADING.md](specs/DYNAMIC_PLUGIN_LOADING.md) | PR-544 — why `.so` loading stays deferred |

## What shipped (high level)

| Track | Highlights |
| --- | --- |
| A | Returns/RMA, B2B customer groups & pricing, payment ledger, store credit API |
| B | Advanced promotions, navigation/blocks API, abandoned cart, reviews |
| C | Admin users/roles/MFA, audit export, shipping zones UI |
| D | Omnibus storefront, WEEE/EPR/GPSR fields, OSS/IOSS export |
| E | Merchant webhooks, plugin CLI, Kafka/SQS queues, GraphQL read API, dynamic-loading research |

## Next phase

[Phase 7 — Customization Platform](../phase-7-customization-platform/ROADMAP.md) implements extension fields, hooks, slots, and plugin assets (PR-700–720).
