# Phase 12 — Product Types & Composition

## Strategy

Phase 11 made background subsystems (jobs, search, cache) reachable by an operator. Phase 12 is a catalog-domain phase: it gives products an actual type taxonomy, fixes a real visibility/purchasability gap that predates this phase, and adds two new relationship mechanisms — catalog-time composition (bundle, grouped) and a many-to-many linked-child relationship with instance-level assignment.

- **Foundation before features.** `Type` (Track A) and the visibility/purchasability model (Track B) are prerequisites for bundle, grouped, linked, and downloadable — every later track's salability rules are built on `Purchasable`, not a bespoke flag per feature.
- **One PR = one responsibility**, reviewable in ~10–20 minutes, same discipline as Phases 10–11.
- **A single standalone fix ships first** (PR-1048, zero-price products), same pattern as Phase 11's PR-1027 — it's a real, independent bug, not something that should wait on the rest of this phase.
- PR specs live under `prs/` (**PR-1048+**, continuing Phase 11's numbering).
- Individual PR specs at this stage carry Summary/Why/Scope/Out-of-scope/Follow-up only — no `Validation (planned)` or `Documentation updates` sections yet. Those get written when a PR is actually implemented (see PR-1029's Round 4 review in Phase 11 for why a pre-filled "planned validation" section left in a *done* PR is worse than not having one).

Each PR is tagged **`[oss]`** unless noted.

---

## What ships, by subsystem

| Subsystem | Today | End of this phase |
| --- | --- | --- |
| Product type | Only a lifecycle `Status` (draft/active/archived); no simple/virtual/bundle/grouped/configurable/downloadable distinction anywhere. | Explicit `Type` field, enforced at creation, driving shipping/tax requirements and admin form shape. |
| Visibility & purchasability | Effectively unenforced: core product read paths (`List`/`FindByID`/`FindByCategoryID`) apply no status filter at all; cart `AddItem` never checks product status. Only search indexing and the sitemap filter on `Status = active`. | Four independent visibility axes + a purchasable flag, computed by a documented default rule with an explicit per-axis override; enforced consistently across every public/storefront read path (REST and GraphQL) and at cart/checkout entry. |
| Composite products | None — no bundle, no grouped, no product-to-product relationship of any kind beyond `Variant` (SKU variation of one product) and category assignment. | Bundle (fixed or dynamic pricing, required/optional components, stock rollup) and Grouped (independent siblings displayed together) as first-class types, with per-component Omnibus/VAT tracking and single-component returns. |
| Linked products & assignment | None. | A many-to-many catalog-time eligibility relationship (which child products are assignable to which parent products/parent categories) plus an instance-level assignment record created at order/fulfillment time — the mechanism the card/ticket use case needs, generalized beyond it. |
| Downloadable / digital fulfillment | None — every product implicitly requires shipping. | `Downloadable` type with file-asset attachment, expiring/limited download grants, and correct digital-goods VAT handling. |
| Search / CSV / GraphQL / GUI parity | N/A until the above ship. | Every new field and relationship indexed, CSV-round-trippable, exposed with GraphQL read parity, and reachable from the admin GUI. |

---

## Tracks

| Track | Goal | PR range |
| --- | --- | --- |
| **—** | Standalone: allow zero-price products | PR-1048 |
| **A** | Product type foundation | PR-1049–1053 |
| **B** | Visibility & purchasability | PR-1054–1059 |
| **C** | Composite products: bundle & grouped | PR-1060–1066 |
| **D** | Linked products & instance assignment | PR-1067–1073 |
| **E** | Downloadable products & digital fulfillment | PR-1074–1078 |
| **F** | Search / CSV / GraphQL / GUI closeout | PR-1079–1083 |

**Ordering rule:** PR-1048 ships standalone, first — it's an independent pricing-domain fix, not gated on anything else in this phase. Track A before everything else (`Type` is referenced by every later track). Track B before C/D/E (bundle, linked-child, and downloadable salability rules are all built on `Purchasable`, not a bespoke per-feature flag). Tracks C, D, and E have no dependency on each other and can run in parallel once A and B are done. Track F depends on C, D, and E being substantially complete (it indexes/exposes the final field set) **and** on Phase 11 Track B (search indexing) having already shipped, since it extends that index schema rather than reworking it twice.

```text
1048 (allow zero-price products — standalone, ships first)
   │
1049 → 1050 → 1051 → 1052 → 1053                                    (Track A: product type foundation)
   │
   └──> 1054 → 1055 → 1056 → 1057 → 1058 → 1059                     (Track B: visibility & purchasability)
                          │
                          ├──> 1060 → 1061 → 1062 → 1063 → 1064 → 1065 → 1066   (Track C: bundle & grouped)
                          │
                          ├──> 1067 → 1068 → 1069 → 1070 → 1071 → 1072 → 1073   (Track D: linked products & assignment)
                          │
                          └──> 1074 → 1075 → 1076 → 1077 → 1078                (Track E: downloadable)
                                                        │
                                                        └──> 1079 → 1080 → 1081 → 1082 → 1083  (Track F: closeout)
```

---

## Standalone — Allow zero-price products (PR-1048)

**Goal:** unblock a card sellable at 0 before the rest of this phase needs it.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1048 | Allow zero-price products | `pricing.NewPrice` and `pricing.NewPriceSnapshot` both reject any amount that isn't strictly positive (`internal/domain/pricing/price.go`, `price_snapshot.go`). Relax both to non-negative (mirrors `cart.Item.NewItem`, which already only rejects negative). Audit every consumer that assumes a positive price for arithmetic safety — discount percentage math, Omnibus lowest-price comparisons, any divide-by-price calculation — and add explicit zero-handling where an implicit "price > 0" assumption was silently relied on. This is a real, independent bug fix (a merchant cannot price anything at 0 today, free products or not); it ships standalone because nothing else in this phase needs to be a prerequisite for it. |

---

## Track A — Product type foundation (PR-1049–1053)

**Goal:** give every product an explicit, enforced type, without breaking anything that exists today.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1049 | `Type` field + migration | New `catalog.Type` enum: `simple \| virtual \| bundle \| grouped \| configurable \| downloadable`. Migration adds the column with a `NOT NULL DEFAULT 'simple'` backfill — every existing product becomes `simple` with zero behavior change. Domain-level validation on create/update (an unknown type value is rejected at the application layer, not just left as an unenforced string — see PR-1028's own lesson about typed-but-unvalidated string fields). |
| PR-1050 | Type-driven shipping/tax requirements | `virtual` and `downloadable` products never require a shipping method; wire this into the existing shipping-rate calculation so checkout doesn't ask for/charge shipping on a virtual card or a digital download. Lay the groundwork (a `RequiresPhysicalShipping bool` derived from `Type`) that Track E's digital-goods VAT handling (PR-1076) builds on — no VAT logic in this PR, just the shipping-requirement derivation. |
| PR-1051 | Admin API type field support | Product create/update endpoints accept and validate `type`; list/filter endpoints accept a `type` query filter. `bundle`/`grouped`/`configurable`/`downloadable` don't have their full behavior yet (later tracks) — this PR is the field plumbing only, matching how PR-1049's migration ships the column before any track uses it. |
| PR-1052 | CSV import/export: type field | Extend the existing product import/export pipeline (`internal/application/importer`, `internal/application/exporter`) with the `type` column; invalid/unknown values rejected with the same validation-failure reporting the importer already uses for other fields. |
| PR-1053 | Admin GUI: type selector + type-specific sections | Product edit form gets a type selector (schema-driven forms/grids pattern, no new frontend framework); type-specific form sections (bundle components, grouped members, linked-child eligibility, downloadable files) are added incrementally by their own tracks — this PR only wires the selector and the section-visibility switch. |

---

## Track B — Visibility & purchasability (PR-1054–1059)

**Goal:** replace "effectively unenforced" with a real, documented, per-axis model — and stop returning/selling draft and archived products today, independent of anything else in this phase.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1054 | Enforce `Status = active` in core read paths | Standalone-shippable bug fix, same spirit as PR-1048: `internal/infrastructure/postgres/product_repo.go`'s `List`/`FindByID`/`FindByCategoryID` apply no status filter at all today, and `cart.Service.AddItem` never checks product status — a `draft`/`archived` product is publicly readable and addable to cart right now. Fix both. This ships first in the track because it's correct regardless of whether the rest of Track B lands. |
| PR-1055 | Four-axis visibility model | New fields: `VisibleInCatalog`, `VisibleInSearch`, `VisibleIndividually` (direct PDP access), `Purchasable` — each independently computed by default (`Status = active AND quantity > 0 AND a price exists AND assigned to ≥ 1 store/view`; category assignment as a config-level toggle, off by default, since many legitimate products are intentionally uncategorized) — plus a per-axis `VisibilityMode: auto \| visible \| hidden` override for merchants who need to force a value against the computed default in either direction. |
| PR-1056 | Admin vs. public/storefront API split | Formalize the boundary: admin API continues to see every product regardless of computed/overridden visibility (as today); a new public/storefront-read boundary (REST) enforces all four axes consistently. Audit every existing storefront-facing route for which axis it should be checking (PDP → `VisibleIndividually`, category listing → `VisibleInCatalog`, cart `AddItem` → `Purchasable`) and centralize the filtering logic in one place rather than reimplementing it per handler. |
| PR-1057 | Enforce visibility in storefront read paths | Apply PR-1056's boundary to product listing, PDP, category listing, and sitemap generation (which already partially does this via `Status`) — consistently, not just where it happened to already exist. |
| PR-1058 | Cart/checkout purchasability gate + hook point | `cart.Service.AddItem` rejects a non-`Purchasable` product/variant with a clear `apperror` (not just "no price found," today's incidental gate). Reuses the existing `HookCartAddItemBefore`/`HookCartValidate` plugin hook points (already wired into `AddItem`) as the extension seam Track D's "not purchasable until assigned" rule and any future plugin-driven purchasability rule build on. |
| PR-1059 | Visibility admin GUI | Per-axis toggle + override-mode indicator (computed vs. forced) on the product edit form; a visible "why is this hidden" explanation surfaced next to the toggle when the computed default is what's driving a `hidden` state, so merchants don't have to guess which of the four conditions failed. |

---

## Track C — Composite products: bundle & grouped (PR-1060–1066)

**Goal:** catalog-time composition — a bundle (fixed set of components sold as one unit) and a grouped display (independent siblings shown together) — with correct pricing, stock, tax, and returns behavior, not just a UI grouping.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1060 | Bundle domain model | `bundle_component(bundle_product_id, component_product_id, quantity, required bool)`. A bundle is one order line by default (see PR-1064 for the itemized alternative). Nesting (a bundle containing another bundle) is explicitly out of scope — see "Out of scope" below. |
| PR-1061 | Grouped domain model | `grouped_member(group_product_id, member_product_id)` — members are fully independent, separately priced, separately orderable products; grouping only affects how they're displayed together, never their individual purchasability. This is deliberately *not* what the card/ticket use case needs (see Track D) — grouped members have no dependency on each other or on the parent. |
| PR-1062 | Bundle/grouped pricing rollup | Bundle: `fixed` (merchant sets one price, ignoring component prices) or `dynamic` (sum of selected components, optionally minus a bundle-level discount) — both integrate into the existing pricing pipeline (`BasePriceStep` → plugin steps → `FinalizeStep`) as a bundle-aware base-price step, not a parallel pricing path. Grouped: no parent price at all, only members are priced — matches Track A's "configurable has no direct parent price either" pattern. Depends on PR-1048 (zero-price products) for the case where a dynamic bundle's selected components sum to 0. |
| PR-1063 | Stock rollup per type | Bundle: `fixed` stock (bundle has its own inventory row, independent of components) or `dynamic` stock (derived as the minimum available quantity across required components) — merchant-configurable per bundle, matching the pricing mode split in PR-1062. Grouped: no parent stock; availability is per-member only. |
| PR-1064 | Order line explosion + returns integration | Per-bundle config flag: sell as one order line (default) or itemize into one order line per component (needed where tax/invoice rules require per-item granularity). Itemized bundles extend the existing Returns/RMA domain (Phase 6) to support returning a single component without touching the rest of the bundle's order lines. |
| PR-1065 | Omnibus/VAT per component line | Extend `PriceSnapshot` (lowest-30-day-price tracking for EU Omnibus) to component-level granularity when a bundle is sold itemized (PR-1064) — a component needs its own price history independent of the bundle's headline price. Groundwork shared with Track E's digital-goods VAT handling. |
| PR-1066 | Bundle/grouped admin API + CSV + GUI | Component/member picker (admin API + CSV import/export for `bundle_component`/`grouped_member` rows) and the corresponding admin GUI section on the product edit form (wired via Track A's PR-1053 section-visibility switch). |

---

## Track D — Linked products & instance assignment (PR-1067–1073)

**Goal:** the mechanism the card/ticket use case actually needs — a many-to-many catalog-time eligibility relationship, plus instance-level assignment created at order/fulfillment time. Deliberately not modeled as Bundle or Grouped: a ticket product must remain independently purchasable-in-principle (gated by `Purchasable`, not composed into a fixed set), and the same ticket *product* can end up assigned to many different card *instances* across many different orders — the opposite of a bundle's fixed, catalog-time-only composition.

| PR | Title | Short description |
| --- | --- | --- |
| PR-1067 | `LinkedProduct` catalog eligibility | Many-to-many eligibility, not composition: which child products are assignable to which parent products. Two scoping mechanisms, both supported (merchant picks whichever fits): an explicit parent-product-ID allowlist per child, or a category-based rule (a ticket is eligible for assignment to any product in the "Cards" category) — reuse the existing catalog-rule scoping pattern from Promotions (`internal/application/pricing/catalog_promotion_step.go`) rather than inventing a second rule engine. |
| PR-1068 | `RequiresLinkedParent` purchasability gate | A product flagged `RequiresLinkedParent = true` computes `Purchasable = false` (Track B) until an assignment context makes it eligible — reuses Track B's `HookCartAddItemBefore`/`HookCartValidate` seam (PR-1058) rather than a bespoke check. This is the direct answer to "a ticket must not be sellable or visible until assigned to a card." |
| PR-1069 | `ProductAssignment` instance domain | New domain: binds one purchased/issued child unit (an order item, or a serialized unit for a digital "smart card in a phone" case) to one purchased parent unit. State machine: `unassigned → assigned → revoked` (revoke, not delete — keeps an audit trail; a revoked ticket can be reassigned). This is the piece with no existing analog anywhere in the codebase — closest precedent in shape (not in domain) is the store-credit ledger's append-only entry model. |
| PR-1070 | Assignment flow: cart-time + post-purchase | Both paths supported, per the actual use case (many buyers already own a card and buy tickets for it later; some buy card+ticket together): cart-time assignment (customer selects which of their existing card instances a ticket is being purchased for, if any) is optional; if not chosen at cart time, the ticket is purchasable but the resulting order item is `unassigned` until a post-purchase assignment step (self-service account page or admin action) completes it — a `RequiresLinkedParent` item stays non-purchasable at the *product* level (PR-1068) but an already-purchased-and-unassigned unit is a different state, tracked by PR-1069. |
| PR-1071 | Admin API for assignment | List/create/revoke assignments; "this card's assigned tickets" and "this ticket's current card" views. |
| PR-1072 | Returns integration for assigned children | Return a single assigned ticket without touching the card's order line — extends Returns/RMA (Phase 6) the same way PR-1064 did for bundle components, at assignment granularity instead of bundle-component granularity. |
| PR-1073 | CSV import/export + admin GUI | Bulk eligibility-rule import/export; card detail page showing assigned tickets, ticket detail showing its current card (if any), assign/revoke actions. |

---

## Track E — Downloadable products & digital fulfillment (PR-1074–1078)

**Goal:** a `Downloadable` type with real file delivery, expiry, and correct digital-goods tax handling — not just "virtual with a file attached."

| PR | Title | Short description |
| --- | --- | --- |
| PR-1074 | `Downloadable` type + file asset attachment | Reuses the existing Media domain/storage (local or S3-compatible, per the existing `media.Storage` port) for the underlying file — this PR is the product-to-asset attachment and the `Type = downloadable` behavior (no shipping, per PR-1050), not a new storage mechanism. |
| PR-1075 | `DownloadGrant` domain | Created when an order containing a downloadable item is paid. Carries an expiry (configurable window from grant creation) and a download-count limit. Not tied to `ProductAssignment` (Track D) — a separate, simpler grant model since downloadable delivery has no "assign to a parent" step. |
| PR-1076 | Digital-goods VAT/tax category | EU digital goods have different VAT treatment (reverse charge / OSS) than physical goods — extends the existing EU compliance module (Omnibus/OSS groundwork, `docs/phase-5-maturity/specs/COMPLIANCE_EU.md`) rather than adding a parallel tax path. Consumes PR-1050's `RequiresPhysicalShipping` derivation to decide which tax category applies. |
| PR-1077 | Storefront download delivery endpoint | Auth-gated, signed/token URL (not a permanent public link) that checks the requesting customer's `DownloadGrant` state (not expired, under the count limit) before streaming/redirecting to the underlying asset. |
| PR-1078 | Admin API + CSV + GUI for downloadable management | File attach/replace, grant list/revoke per customer, admin GUI section (wired via PR-1053's section-visibility switch). |

---

## Track F — Search / CSV / GraphQL / GUI closeout (PR-1079–1083)

**Goal:** every field and relationship this phase adds is indexed, CSV-round-trippable, exposed with GraphQL read parity, and documented for operators — not left as a REST-admin-only feature.

**Hard dependency:** Phase 11 Track B (search indexing rework) must have already shipped — this track extends that index schema, it does not rework it a second time (see the forward-compatibility note added to [Phase 11's `ROADMAP.md`](../phase-11-jobs-search-cache/ROADMAP.md#forward-compatibility-phase-12-product-types--composition)).

| PR | Title | Short description |
| --- | --- | --- |
| PR-1079 | Search index schema extension | Add `Type` and the visibility axes to what Phase 11's indexer writes; filter search/catalog results on `Purchasable`/`VisibleInSearch`/`VisibleInCatalog` so an unassigned linked-child (Track D) or a hidden product (Track B) never appears in results. |
| PR-1080 | On-save reindex trigger generalization | Extend Phase 11 PR-1036's on-save reindex subscriber list to also fire on visibility-affecting changes introduced by this phase: a linked child getting assigned/revoked, a bundle's rollup stock crossing zero, a downloadable grant issued (if grant state ever affects catalog visibility — confirm during implementation; likely not, since `DownloadGrant` is post-purchase, not catalog-facing). |
| PR-1081 | CSV import/export final consistency pass | One pass across every existing import/export pipeline (products/variants/prices/stock/attributes/categories) confirming every field this phase added (type, visibility axes + override mode, bundle/grouped components, linked eligibility, assignments) round-trips correctly and consistently, not just within its own track's PR. |
| PR-1082 | GraphQL read parity | Extend `plugins/core/graphql`'s existing product schema (`schema.go`'s `productType`/`products`/`productBySlug` resolvers) with `Type`, the visibility axes, and bundle/grouped/linked-child relationships — matches Phase 7 Track E's established "same operation via admin REST, GraphQL, and plugin registration" parity precedent. Read-only, matching what the GraphQL surface already is today. |
| PR-1083 | RUNBOOK.md + admin GUI consolidation pass | Symptom → check → fix RUNBOOK.md entries ("why can't I add this to cart," "why is this product not visible," "why isn't my ticket sellable"), same style as Phase 10's incident-response section. Final admin GUI pass folding all of this phase's sections into the existing product edit screen tabs, confirming nothing was left as an API-only feature with no GUI path. |

---

## Design notes: open decisions worth stating explicitly

- **Card/ticket assignment timing (Track D).** Both cart-time and post-purchase assignment are supported by design, not because it was unclear which one to pick — real usage needs both (a customer topping up an already-owned card is a different flow from buying card+ticket together). PR-1070 is where this gets implemented; the state model in PR-1069 needs to support an order item existing in an `unassigned` state without that meaning "not purchasable," which is a different flag entirely (`Purchasable`, Track B) evaluated at a different time (before purchase, not after).
- **Linked-child eligibility scoping (Track D).** Both an explicit product-ID allowlist and a category-based rule are supported (PR-1067) — reusing Promotions' existing catalog-rule scoping rather than building a second rule engine keeps this from becoming a bespoke mechanism.
- **GraphQL is schema extension, not new infrastructure (Track F).** Phase 11 explicitly scoped its *new* jobs/cache/scheduler admin endpoints as REST-only. That decision doesn't apply here: `plugins/core/graphql` already has a product query surface (Phase 7 Track E), so PR-1082 is additive parity work on an existing surface, the same category of work Phase 7 already did for extension fields.
- **Ticket pools are explicitly out of scope** (see below) — flagged during planning specifically so it's a deliberate exclusion, not a gap nobody noticed.

---

## Out of scope (explicit)

| Item | Why deferred |
| --- | --- |
| Ticket pools (bulk-allocate N tickets as one purchasable unit, distributed internally) | Two workarounds exist without new core mechanism: integrator-side (the pool is managed outside Shopanda, individual tickets synced in via the existing integration REST/plugin SDK) or an extension-field attribute on a batch of `ProductAssignment` records. Revisit only if a concrete merchant need for a *core* pool concept surfaces. |
| Bundle nesting (a bundle containing another bundle as a component) | Real added complexity (recursive pricing/stock rollup, cyclic-reference validation) for a need that hasn't come up yet. |
| Subscription / recurring billing for cards or tickets | A materially larger feature (recurring payment capture, renewal/dunning flows) than product composition; this phase is one-time purchase + assignment only. |
| Cross-store / marketplace linked products (a linked child fulfilled by a different merchant) | No `store_id` on `products`/`categories` today (Phase 11 already flagged this same gap as out of scope for search-index partitioning) — cross-store linking is blocked on a bigger, separate multi-tenant catalog change. |
| A visual bundle/kit "builder" UI beyond the schema-driven admin grid pattern | Matches Phase 11's own constraint: extends the existing bundled admin SPA, does not introduce a new frontend framework. |

---

## Effort estimate (calendar, one focused engineer)

| Track | Effort | Notes |
| --- | --- | --- |
| Standalone (PR-1048) | 1–2 days | Small, well-understood fix; the audit-for-implicit-positive-price-assumptions part is the only real unknown. |
| A | 1 week | Mostly plumbing once the enum and migration land. |
| B | 1.5–2 weeks | PR-1055's default-visibility rule and PR-1056's API-surface split are the judgment-heavy parts — get the four-axis default rule reviewed carefully, it's the thing every later track depends on being right. |
| C | 2–2.5 weeks | PR-1062–1065 (pricing/stock rollup, order-line explosion, per-component Omnibus/VAT) are individually small but each touches a different existing subsystem — expect more cross-team review than lines-of-code would suggest. |
| D | 2.5–3 weeks | Highest-judgment track in this phase — PR-1069's instance-assignment state model and PR-1070's dual assignment-timing flow have no existing precedent in the codebase to lean on, unlike C's tracks which extend pricing/stock/tax systems that already exist. |
| E | 1.5–2 weeks | PR-1076 (digital-goods VAT) is the part most likely to need real tax-domain review, not just engineering review. |
| F | 1–1.5 weeks | Mechanical once A–E are done, gated on Phase 11 Track B shipping first. |

**Total:** ~11–14 weeks. Tracks C, D, and E can run in parallel once A and B ship, so wall-clock can compress meaningfully with more than one engineer — D is the long pole if only one engineer is available.

---

## PR index (quick reference)

| PR | Track | Status |
| --- | --- | --- |
| 1048 | — | planned |
| 1049–1053 | A | planned |
| 1054–1059 | B | planned |
| 1060–1066 | C | planned |
| 1067–1073 | D | planned |
| 1074–1078 | E | planned |
| 1079–1083 | F | planned |

PR specs: [`prs/`](prs/).

---

## Relationship to prior phases

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 10 | Platform excellence (quality / security / ops / architecture) | Shipped (PR-1000–1026; PR-1003 the only open item, unrelated to this phase) |
| Phase 11 | Jobs, search & cache — admin reachability + full-page cache | In progress |
| **Phase 12** | Product types & composition | **Planned** |
