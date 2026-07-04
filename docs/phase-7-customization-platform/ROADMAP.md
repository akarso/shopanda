# Phase 7 — Customization Platform

## Strategy

* Build on **Phase 6 merchant-complete admin** — core commerce and extension hooks exist; Phase 7 unifies **custom data, rendering, and assets**
* **Explicit extension only** — registry, ACL, deterministic ordering; no reflection magic or core overrides
* Prefer **vertical slices** (domain → persistence → HTTP → one consumer) over big-bang platform dumps
* One PR = one responsibility; runnable and reviewable in ~10–20 minutes
* PR specs live under `prs/` (planned range **PR-700–710**)

Each PR is tagged **`[oss]`** unless noted. See [Commercial licensing](../COMMERCIAL.md).

---

## Licensing split (OSS vs plugins)

| Tag | Where it ships | License |
| --- | --- | --- |
| **`[oss]`** | `internal/domain`, `internal/application`, admin/storefront wiring | GPL v3 |
| **Plugin** | `plugins/*` reference implementations | Plugin author's license |

Open core must build and pass CI **without** any reference plugin enabled.

---

## Starting Point (after Phase 6)

Phase 6 closed admin UI debt. Merchants operate from the embedded admin SPA; developers extend via compile-time plugins, events, pipelines, and workflows.

**What works today**

| Area | Status |
| --- | --- |
| Catalog attributes | First-class `catalog.Attribute` + admin CRUD; product form integration |
| Plugins | Register pipelines, events, config, CLI, GraphQL (read) |
| Storefront | SSR templates, content blocks, DB navigation |
| Admin | Full sidebar coverage; no placeholder routes |

**Gaps motivating Phase 7**

| Gap | Impact |
| --- | --- |
| No unified **extension field** model | Plugins invent ad hoc JSON columns or duplicate attribute patterns |
| **Private vs public** custom data | Merchants see fields they should not edit; no ACL on custom values |
| **Cart → order** custom data | No standard snapshot policy for line-item extensions |
| **Hooks/slots** are static | New injection points require core template edits |
| **CSS/JS snippets** | Require theme fork or inline hacks |
| GraphQL | No extension field/value surface |

Phase 6 backlog explicitly scheduled **Customization platform** for Phase 7+. See [CUSTOMIZATION_PLATFORM.md](specs/CUSTOMIZATION_PLATFORM.md).

---

## Target Outcome — "Customization Platform"

When Phase 7 ships, a developer **without core forks** should be able to:

- Register a **namespaced extension field** (public or private) on `product`, `cart_item`, or `order_item`
- Read/write values via **admin REST**, **GraphQL**, and **plugin service APIs**
- Show public fields in a generic **Extensions** panel on product edit
- **Capture** cart line extensions and **snapshot** to order lines at checkout
- Register a **dynamic hook** (e.g. `cart.add_item.after`) and **slot renderer** without editing core enums
- Inject **plugin CSS/JS** on selected routes with deterministic order and CSP nonce support

Merchants edit **public** fields in admin; **private** fields stay hidden unless privileged.

---

## Tracks

| Track | Goal | PR range | Delivers |
| --- | --- | --- | --- |
| **A** | Field registry | PR-700–702 | Domain model, persistence, admin definition CRUD |
| **B** | Values & product UX | PR-703–704 | Value storage/API, product Extensions admin panel |
| **C** | Commerce lifecycle | PR-705–706 | Cart line capture, checkout snapshot to order |
| **D** | Hooks & slots | PR-707–708 | Dynamic hook catalog, slot markers + renderers |
| **E** | Assets & GraphQL | PR-709–710 | Plugin asset manifest, GraphQL parity |

Recommended order: **A (700–702)** → **B (703–704)** → **C (705–706)** → **D (707–708)** → **E (709–710)**.

---

## Track A — Field Registry

**Goal:** Namespaced field definitions with validation metadata and plugin registration port.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-700 | [oss] | Extension field domain + registry | `ExtensionField` model, in-process registry, validation types, plugin register port |
| PR-701 | [oss] | Field definition persistence | Migration + Postgres repo; soft-delete; list by scope/target |
| PR-702 | [oss] | Admin field registry API | CRUD `/api/v1/admin/extensions/fields`; RBAC; private field ACL on read |

**Out of scope:** Value storage, admin field-definition UI grid (stretch), runtime `.so` plugins.

---

## Track B — Values & Product UX

**Goal:** Durable values with ACL and first merchant-facing editor.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-703 | [oss] | Extension value storage + API | JSONB envelope repo; upsert/batch GET/PUT/DELETE; `forbidden_private_field` errors |
| PR-704 | [oss] | Product Extensions admin panel | Generic public-field widgets on product edit; save via value API |

**Out of scope:** Custom widget per field code (plugin hook later), variant-level fields (stretch).

---

## Track C — Commerce Lifecycle

**Goal:** Values flow cart → checkout → order deterministically.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-705 | [oss] | Cart item extension capture | Write/read on cart lines; expose in cart API + storefront cart view |
| PR-706 | [oss] | Checkout order-item snapshot | Copy declared `snapshot` fields cart_item → order_item in checkout workflow |

**Out of scope:** Reversible order edits, refund-time extension mutations.

---

## Track D — Hooks & Slots

**Goal:** Dynamic registration without core template forks per injection point.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-707 | [oss] | Dynamic hook registry | Plugin register hook handlers; catalog endpoint; ordered chain execution |
| PR-708 | [oss] | Slot registry + storefront markers | Template slot markers; renderer registry; deterministic render on PDP/cart/checkout |

**Out of scope:** Full page builder, hook debugging UI.

---

## Track E — Assets & GraphQL

**Goal:** Frontend snippets and API parity for headless consumers.

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-709 | [oss] | Plugin asset manifest injection | CSS/JS registration; route gating; CSP nonce; load order |
| PR-710 | [oss] | GraphQL extension parity | Query/mutate fields + values with same ACL as REST |

**Out of scope:** CDN upload pipeline, admin asset editor.

---

## Additional Backlog (unscheduled, Phase 7+)

| Theme | Examples |
| --- | --- |
| **Reference plugin** | Custom dropdown on PDP → cart → order E2E demo |
| **Admin registry UI** | Grid CRUD for field definitions (today API-only in 702) |
| **Plugin SDK** | `plugin.App.Extensions()` helpers wrapping registry |
| **Docs** | DEVELOPER.md + PLUGINS.md extension platform chapter |
| **Variant/customer scopes** | Extend entity matrix beyond product/cart/order |

---

## Explicitly Out of Scope (Phase 7)

* Runtime Go `.so` plugin loading ([PR-544 research](../phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md))
* Visual drag-and-drop page builder
* Low-code rule engine
* Replacing catalog attributes for standard merchandising (attributes remain; extensions complement)
* SaaS multi-tenancy

---

## Validation Target

### Developer (acceptance from spec §13)

- Add a custom **product** field without template override
- Value flows **cart → order** via snapshot policy
- Same operation via **admin REST**, **GraphQL**, and plugin registration
- **Private** fields hidden from default admin
- New **hook/slot** without core enum edit
- **CSS/JS** mounted without full custom theme

### Architecture

* Domain has no HTTP/DB imports
* All writes go through registry service (no bypass)
* Private field values never logged in audit/plaintext
* One PR = one layer or one vertical slice consumer

---

## PR Index (quick reference)

| PR | Track | License | One-liner |
| --- | --- | --- | --- |
| 700 | A | [oss] | Extension field domain + registry |
| 701 | A | [oss] | Field definition persistence |
| 702 | A | [oss] | Admin field registry API |
| 703 | B | [oss] | Extension value storage + API |
| 704 | B | [oss] | Product Extensions admin panel |
| 705 | C | [oss] | Cart item extension capture |
| 706 | C | [oss] | Checkout order-item snapshot |
| 707 | D | [oss] | Dynamic hook registry |
| 708 | D | [oss] | Slot registry + storefront markers |
| 709 | E | [oss] | Plugin asset manifest injection |
| 710 | E | [oss] | GraphQL extension parity |

PR specs: [`prs/`](prs/).

---

## Relationship to Prior Phases

| Phase | Focus | Status |
| --- | --- | --- |
| [Phase 1](../phase-1-core/ROADMAP.md) | Core engine | Shipped |
| [Phase 2](../phase-2-merchant-ready/ROADMAP.md) | Merchant-ready surfaces | Shipped |
| [Phase 3](../phase-3-testing/ROADMAP.md) | Hardening & guest checkout | Shipped |
| [Phase 4](../phase-4-refactoring/ROADMAP.md) | Product complete | Shipped |
| [Phase 5](../phase-5-maturity/ROADMAP.md) | Mature commerce | Shipped |
| [Phase 6](../phase-6-merchant-complete/ROADMAP.md) | Merchant-complete admin | Shipped |
| **Phase 7** | **Customization platform** | **Planned** |
