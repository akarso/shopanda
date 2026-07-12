# Phase 8 — Integrator Platform

**Status:** Track A **complete** (PR-800–802). Track B+ planned.

Phase 8 closes the gap between **Magento-level customization freedom** and **Go-native explicit wiring**. Integrators and agencies must be able to change cart and pricing behavior, transform CSV imports before persistence, expose REST endpoints for ERP systems (SAP, etc.), and call external GraphQL/REST APIs (warehouse, PIM) — **without forking core**.

Phase 7 delivered extension fields, hooks/slots, and storefront DX. Phase 8 delivers **commerce behavior chains**, **import pipelines**, and **inbound/outbound integration seams**.

## Roadmap

- [**ROADMAP.md**](ROADMAP.md) — tracks A–F, PR index, validation targets
- [**Integrator Platform spec**](specs/INTEGRATOR_PLATFORM.md) — design position, port catalog, integration patterns
- [**PR specs**](prs/README.md) — per-PR implementation notes (planned PR-800+)

## Upstream

- [Phase 7 — Customization Platform](../phase-7-customization-platform/ROADMAP.md) (complete)
- [Plugin composition guide](../guides/PLUGIN_COMPOSITION.md)
- [Extension API policy](../guides/EXTENSION_API.md)
- [Dynamic plugin loading research](../phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md) — compile-time plugins remain the model

## Who this phase is for

| Persona | Phase 8 delivers |
| --- | --- |
| **Integrator / agency** | CSV row transforms, ERP webhooks, warehouse sync, PIM enrichment — documented patterns, not ad hoc core patches |
| **Plugin author** | Positioned pricing/cart steps, import hooks, integration auth, outbound job registration |
| **Core maintainer** | Port catalog, precedence policy, introspection — one answer per extension question |

## Explicit non-goals

- Magento-style `di.xml`, override folders, or runtime class preferences
- Runtime Go `.so` plugin loading
- Visual rule builder / low-code promotion designer
- Layered navigation, faceted search, or “install any npm package” theming (separate tracks)
