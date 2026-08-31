# Phase 12 — Product Types & Composition

Give the catalog a real product-type model (simple, virtual, bundle, grouped, configurable, downloadable), a proper multi-axis visibility/purchasability model, and two product-to-product relationship mechanisms — catalog-time composition (bundle/grouped) and a many-to-many "linked child" relationship with instance-level assignment, the mechanism a parent "card" + child "ticket" catalog needs.

## Why this phase

A conversation about a concrete use case — a sellable "card" product (simple or virtual) that free-standing "ticket" products get assigned to, where a ticket must not be independently sellable or visible until it's assigned — surfaced that the current catalog model can't express it, and exposed several gaps along the way that matter beyond that one use case:

- **No product-type concept at all.** `catalog.Product` has only a lifecycle `Status` (draft/active/archived); there's no simple/virtual/bundle/grouped/configurable/downloadable distinction anywhere in the domain, API, or admin.
- **No product-to-product relationship** other than `Variant` (SKU/attribute variation of one product, not a separate sellable product) and category assignment. Nothing lets one product reference another as a component, a sibling, or a dependency.
- **Visibility/purchasability is barely enforced today**, and where it exists it's a single implicit gate, not a real model: `internal/infrastructure/postgres/product_repo.go`'s `List`/`FindByID`/`FindByCategoryID` run with **no status filter at all** — a `draft`/`archived` product is returned by the public product API and is addable to cart, because `cart.Service.AddItem` never checks product status either. Only the search index and the sitemap filter on `Status = active` today.
- **Zero-price products can't be created.** `pricing.NewPrice` rejects any amount that isn't strictly positive — a card that should be sellable at 0 hits a validation error before there's even a visibility question to answer.

This phase closes all four gaps as one coherent piece of work: the type field and visibility model are the foundation everything else (bundle, grouped, linked, downloadable) is built on, not four independent patches.

## Design principles carried through every track

- **Two API surfaces, not one filtered-by-convention surface.** Admin API stays unrestricted (sees every product regardless of status/visibility, as today). Public/storefront-facing reads — REST and GraphQL alike — enforce visibility and purchasability consistently; in a headless deployment this API *is* the storefront, so there's nothing to "ignore" the visibility model for.
- **Visibility is four independent axes, not one flag.** `VisibleInCatalog`, `VisibleInSearch`, `VisibleIndividually` (direct PDP access), and `Purchasable` are computed independently from a default rule (see Track B), with an explicit `VisibilityMode: auto | visible | hidden` override per axis for the cases the default rule gets wrong for a specific merchant.
- **Catalog-time composition and instance-time assignment are different mechanisms, not one generalized "relationship" type.** Bundle/grouped (Track C) are merchant-defined at catalog time and identical for every buyer. Linked products (Track D) are a many-to-many catalog-time *eligibility* relationship plus a separate, instance-level assignment record created at order/fulfillment time — a ticket product isn't "in a bundle with" a specific card, it becomes eligible for assignment to *any* compatible card, and a specific purchased ticket gets bound to a specific purchased card afterward.
- **Extend the existing pricing/stock/tax/returns pipelines, don't fork them.** Bundle pricing rollup goes through the existing pricing pipeline; bundle/linked-child returns go through the existing Returns/RMA domain at line-item granularity; digital-goods tax handling extends the existing EU compliance module. No parallel systems.
- **GraphQL gets the same read parity REST gets, because it already does.** `plugins/core/graphql` already exposes product queries (Phase 7 Track E's "same operation via admin REST, GraphQL, and plugin registration" precedent) — extending its schema with the new fields is a schema addition, not new infrastructure. This is a different question from Phase 11's "REST only for the new jobs/cache/scheduler admin endpoints" decision — that was about *new admin surfaces* with no existing GraphQL presence; this is about the *product catalog*, which already has one.

See [`ROADMAP.md`](ROADMAP.md) for the full track breakdown, sequencing, and PR specs under [`prs/`](prs/).

## Status

**Planned.** Not started — picked up after Phase 11 finishes. Track F (search/GraphQL closeout) has a hard dependency on Phase 11 Track B (search indexing) having shipped first.

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 11 | Jobs, search & cache — admin reachability + full-page cache | In progress |
| **Phase 12** | Product types & composition — type model, visibility, bundle/grouped, linked products & assignment, downloadable | **Planned** |
