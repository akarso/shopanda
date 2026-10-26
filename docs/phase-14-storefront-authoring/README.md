# Phase 14 — Storefront authoring

Finish the existing SSR theme model and close the headless gaps that blocked a custom frontend. No new templating language, no official React storefront, no embedded themes.

## Why this phase

Custom SSR themes already existed (child `parent:`, slots, fragments) but felt unfinished: missing htmx file, no static inheritance, restart-only template edits, spec drift.

Headless was architecturally intended (`frontend.enabled: false`) but not productized: no CORS, UUID-only product GET, no address JSON, claim routes gated on SSR, OpenAPI/examples missing.

## Design principles

- Two clients: Go `html/template` and REST. Not a third JS framework.
- Install shape: binary + `themes/` directory (Hugo model).
- Pin htmx 1.9.12. Do not vendor Alpine or chase htmx 2.x.
- Storefront product/category JSON keeps Go names by default; snake_case is opt-in. Admin domain encoding is unchanged.

See [`ROADMAP.md`](ROADMAP.md) and [`prs/`](prs/).

## Status

**Shipped** (PR-1100–1110).

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 7 Track F | Slots, inheritance, partials | Shipped |
| Phase 2 | Default storefront templates | Shipped |
| **Phase 14** | Theme packaging + headless completeness | **Shipped** |
