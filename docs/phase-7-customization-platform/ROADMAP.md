# Phase 7 — Customization Platform

## Strategy

* Build on **Phase 6 merchant-complete admin** — core commerce and extension hooks exist; Phase 7 unifies **custom data, rendering, and assets**
* **Explicit extension only** — registry, ACL, deterministic ordering; no reflection magic or core overrides
* Prefer **vertical slices** (domain → persistence → HTTP → one consumer) over big-bang platform dumps
* One PR = one responsibility; runnable and reviewable in ~10–20 minutes
* PR specs live under `prs/` (**PR-700–720** shipped)

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
| **F** | Storefront DX | PR-711–720 | Slot catalog, theme inheritance, partials, dev tooling; **no** Magento layout XML |

Recommended order: **A (700–702)** → **B (703–704)** → **C (705–706)** → **D (707–708)** → **E (709–710)** → **F (711–720)** for maximum storefront developer satisfaction.

---

## Track F — Storefront Developer Experience (PR-711–720)

**Goal:** Maximum developer satisfaction for plugin authors and theme authors — **without** Magento-style layout XML.

Phase 7 Tracks A–E shipped the registry, hooks, slots, and assets. Track F closes the **ergonomics gap**: predictable anchors, theme extension without full forks, and tooling that makes misconfiguration obvious early.

### Design position (explicit non-goals)

| Approach | Decision |
| --- | --- |
| Magento `layout.xml` merge tree | **Out of scope** — high complexity, poor debuggability, fights explicit-wiring philosophy |
| Visual drag-and-drop page builder | **Out of scope** — separate product surface (merchant CMS), not plugin DX |
| Full DOM reordering by plugins | **Out of scope** — plugins inject; themes own structure |
| **Slots + asset manifest + hooks** | **Keep** — additive injection, deterministic order, testable |
| **Canonical anchor catalog** | **In scope** — convention over configuration |
| **Theme inheritance + partials** | **In scope** — extend default theme, preserve slot markers |
| **Declarative block order within fixed containers** | **Stretch (PR-716)** — `layout.yaml` reorder only, not arbitrary moves |

**Personas**

| Persona | Track F delivers |
| --- | --- |
| Plugin author | Registers renderer against a **documented anchor**; dev mode warns if theme has no marker |
| Theme author | Overrides one partial or child template; **inherits** parent slot markers |
| Merchant (no Go) | Still uses admin content blocks + public extension fields; not layout surgery |

### PR plan

| PR | License | Title | Short description |
| --- | --- | --- | --- |
| PR-711 | [oss] | Standard slot catalog | `layout.*` (head, header, nav, main, footer, body_end), `pdp.*`, `plp.*`, `checkout.*`, `cart.*` in default theme; catalog in `PLUGINS.md` |
| PR-712 | [oss] | Theme inheritance + stable API draft | `parent:` in `theme.yaml`; child overrides templates; parent slot markers preserved; draft first public hook/slot API surface (names/constants/types; Stable v0 vs Internal) |
| PR-713 | [oss] | Layout partials | Split `layout.html` into `_header.html`, `_footer.html`, `_nav.html`; custom themes override partials not monolithic layout |
| PR-714 | [oss] | Slot dev ergonomics + stable API v0 | Dev-mode log when plugin registers anchor with no theme marker; `GET /api/v1/admin/extensions/slots`; publish v0 stability policy and compatibility tests for hook/slot contracts |
| PR-715 | [oss] | Remaining page anchors | `account.nav`, `home.hero`, `checkout.panel`, `cart.items`; complete coverage for shipped page types |
| PR-716 | [oss] | `layout.yaml` block ordering (stretch) | Declarative reorder of **named blocks within fixed containers** (e.g. PDP info column); no cross-container moves |
| PR-717 | [oss] | Nested `slot_container` fix | Preprocessor handles nested containers OR enforce lint rule + docs (nested `slot_container` breaks today; use explicit `slot` inside) |
| PR-718 | [oss] | Reference plugin: slots E2E | Demo plugin registers layout + PDP renderers; integration test asserts DOM positions |
| PR-719 | [oss] | Theme author guide | `docs/guides/THEME_SLOTS.md` — anchor catalog, inheritance, partials, plugin coordination |
| PR-720 | [oss] | Slot marker validation (stretch) | Theme load warns on unknown anchor names or documents optional strict mode for CI |

Recommended order: **711 → 712 → 713 → 714 → 715** (core DX), then **718** (proof), then **716–717–719–720** as needed.

### Stable API draft scope (PR-712/714)

Track F includes a first public stable API surface for extension authors, limited to **hooks + slots**:

- **PR-712 (draft surface):**
  - introduce a public Go package for stable contracts (names/constants/types for hook points and slot anchors)
  - keep existing internals as implementation details; add adapters/aliases where needed
  - document what is **Stable v0** vs **Internal**
- **PR-714 (hardening):**
  - publish stability policy (what breaks when, deprecation path)
  - add compatibility guard tests for stable hook/slot contracts
  - ensure tooling endpoints reflect the same canonical names

Non-goal for this draft: freezing all plugin APIs. The v0 scope is intentionally narrow (hooks + slots).

**Out of scope for Track F:** runtime `.so` plugins, admin drag-and-drop layout editor, replacing composition pipelines for API responses.

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
| PR-708 | [oss] | Slot registry + storefront markers | Slot anchors with before/after/prepend/append placements; renderer registry; PDP + cart |

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

| Theme | Examples | Notes |
| --- | --- | --- |
| **Reference plugin** | Custom dropdown on PDP → cart → order E2E demo | Overlaps PR-718; keep one canonical demo |
| **Admin registry UI** | Grid CRUD for field definitions (today API-only in 702) | |
| **Plugin SDK** | `plugin.App.Extensions()` helpers wrapping registry | **Phase 8 Track F** (integrator-focused SDK) |
| **Variant/customer scopes** | Extend entity matrix beyond product/cart/order | |

*(Storefront slot catalog, theme inheritance, layout partials, and slot tooling moved to **Track F** above.)*

## Explicitly Out of Scope (Phase 7)

* Runtime Go `.so` plugin loading ([PR-544 research](../phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md))
* Visual drag-and-drop page builder (merchant CMS is a separate track)
* Magento-style `layout.xml` block merge tree (see Track F design position)
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
- **Standard layout slots** (`layout.header`, `layout.footer`, …) without forking `layout.html` (Track F)
- **Child theme** inherits parent slot markers (Track F)
- Dev tooling surfaces **unbound slot registrations** before production (Track F)

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
| 711 | F | [oss] | Standard slot catalog (default theme) |
| 712 | F | [oss] | Theme inheritance + hook/slot API draft |
| 713 | F | [oss] | Layout partials |
| 714 | F | [oss] | Slot dev ergonomics + stable API v0 |
| 715 | F | [oss] | Remaining page anchors |
| 716 | F | [oss] | `layout.yaml` block ordering (stretch) |
| 717 | F | [oss] | Nested `slot_container` fix |
| 718 | F | [oss] | Reference plugin: slots E2E |
| 719 | F | [oss] | Theme author guide |
| 720 | F | [oss] | Slot marker validation (stretch) |

PR specs: [`prs/`](prs/) (PR-700–720 done).

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
| **Phase 7** | **Customization platform** | **Shipped (PR-700–720; tracks A–F complete)** |
| [Phase 8](../phase-8-integrator-platform/ROADMAP.md) | Integrator platform | In progress (PR-800 done) |
 