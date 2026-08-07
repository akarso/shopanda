# Phase 9 — Integrator Backlog & Merchant Discovery

## Strategy

* Close **Phase 8 unscheduled backlog** and **Phase 7 carryover** before merchant discovery features
* One PR = one responsibility; runnable and reviewable in ~10–20 minutes
* PR specs live under `prs/` (**PR-856+**)

Each PR is tagged **`[oss]`** unless noted.

---

## Starting point (after Phase 8)

Phase 8 shipped integrator seams: positioned pricing/checkout steps, cart hooks, import row hooks, integration REST, sync jobs, plugin SDK, registration report, tax port + reference plugin.

**Remaining from Phase 8 backlog**

| Item | Status |
| --- | --- |
| Export row hooks | Track A — PR-856–858 |
| Shipping rate port | Track A — PR-859 |
| Mail sender port | Track A — PR-860–861 (done) |
| Promotion rule evaluator port | Track A — PR-862 (done) |
| Reference checkout plugin | Track A — PR-863 |
| Admin idempotency UI | Track A — PR-864 (done) |
| `depends_on` init ordering | Track A — PR-865 |

**Remaining from Phase 7 backlog**

| Item | Status |
| --- | --- |
| Extension field admin grid | Track B — PR-866 (done) |
| Variant extension scopes | Track B — PR-867 (done) |

---

## Track A — Integrator backlog (PR-856–865)

| PR | Title | Short description |
| --- | --- | --- |
| PR-856 | Export row hook registry | `exportctx` registry + `app.Export().RegisterRowHook` + extapi |
| PR-857 | Wire core exporters | Invoke export row hooks in `export:*` CLI |
| PR-858 | Reference plugin: export remap | Demo column transform on `export:products` |
| PR-859 | Shipping rate provider port | `RegisterShippingRateProvider` + checkout resolution |
| PR-860 | Mail sender port | `RegisterMailSender` + notification wiring |
| PR-861 | Reference plugin: mail sender | Log-backed `RegisterMailSender` reference (`plugins/maildemo`) |
| PR-862 | Promotion rule evaluator port | Plugin-registered rule types alongside core JSON rules |
| PR-863 | Reference plugin: checkout step | Positioned validation step (`before:create_order`) |
| PR-864 | Admin idempotency UI | List/replay inbound integration idempotency records |
| PR-865 | Plugin `depends_on` | Config-driven init order + cycle detection |

Recommended order: **856 → 857 → 858** → **859 ∥ 860** → **862** → **863** → **864** → **865** (865 can land earlier).

---

## Track B — Phase 7 carryover (PR-866–867)

| PR | Title | Short description |
| --- | --- | --- |
| PR-866 | Extension field admin grid | Admin UI for field definition CRUD (API exists) |
| PR-867 | Variant extension scopes | `target_type=variant` values + admin UX |

---

## Track C — Merchant discovery **PRIORITY**

> **Important:** These four themes are the next major product goals after tracks A and B. Do not omit from future planning or roadmaps.

| PR | Title | Short description | Priority |
| --- | --- | --- | --- |
| PR-900 | Layered navigation | Faceted category/attribute filters on PLP — interactive, out of the box | **IMPORTANT** |
| PR-901 | Advanced search | Filter by attributes marked “use in advanced search”; autosuggest API + storefront | **IMPORTANT** |
| PR-902 | Attribute properties | Extended attribute model (field type, adv. search flag, promo rules, …) + admin UI | **IMPORTANT** |
| PR-903 | Installation UX | Merchant-side setup wizard — approach WordPress-style installer without runtime `.so` loading | **IMPORTANT** |

Track C depends on catalog/search infrastructure (partially exists: search facets, attribute admin, Meilisearch optional).

---

## Track D — Discovery follow-ups (PR-904+)

| PR | Title | Short description |
| --- | --- | --- |
| PR-904 | Attribute facets (PLP) | Layered nav chips for `use_in_layered_nav` attributes (PostgreSQL search) — **done** |
| PR-905 | Advanced search filters | Storefront filter panel + `/api/v1/search` attribute params for `use_in_advanced_search` — **done** |
| PR-906 | Meilisearch attribute parity | `attr_*` filters + facets when `search.engine=meilisearch` — **done** |
| PR-907 | Meilisearch facet hot-reload | Admin discovery-flag changes sync Meilisearch filterable fields — **done** |
| PR-908 | CSV import facet sync | `import:attributes` syncs Meilisearch discovery filterable fields — **done** |

**PR-901** shipped storefront **autosuggest** only; attribute-based advanced search filters are **PR-905**, not PR-901.

---

## PR index (quick reference)

| PR | Track | Status |
| --- | --- | --- |
| 856–860 | A | done |
| 861 | A | done |
| 862 | A | done |
| 863, 865 | A | done |
| 864 | A | done |
| 866 | B | done |
| 867 | B | done |
| 900 | C **PRIORITY** | done |
| 901 | C **PRIORITY** | done |
| 902 | C **PRIORITY** | done |
| 903 | C **PRIORITY** | done |
| 904 | D | done |
| 905 | D | done |
| 906 | D | done |
| 907 | D | done |
| 908 | D | done |

PR specs: [`prs/`](prs/).

---

## Relationship to prior phases

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 8 | Integrator platform | Shipped (PR-800–855) |
| **Phase 9** | Backlog + merchant discovery | **Shipped (PR-856–908)** |
| Phase 10 | Platform excellence (A / A+) | Planned — [`../phase-10-platform-excellence/ROADMAP.md`](../phase-10-platform-excellence/ROADMAP.md) |
