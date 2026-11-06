# Spec / ADR: Public extension SDK (Track E)

- **Status:** Proposed (implement after Tracks A–D are site-ready)
- **Date:** 2026-11-06
- **Related:** Phase 17 Track E, PR-1540+
- **Supersedes:** none (extends `pkg/pluginsdk` / `pkg/extapi` direction from Phase 8/13)

## Context

External plugins today import Shopanda `internal/domain`, `internal/application`, and `internal/platform`. Go’s `internal/` rule forces product code to live under the Shopanda module tree — hence product forks. `pkg/pluginsdk` and `pkg/extapi` already expose a subset of author-facing APIs, but not enough to build a full site plugin without `internal/`.

Tracks A–D make forks tolerable. Track E removes the permanent fork tax.

## Decision (target)

1. Publish a **stable public extension surface** (expand `pkg/pluginsdk` / `pkg/extapi` and/or a dedicated module) that covers ports product plugins need: registration, Migrator, HTTP routes, sync jobs, config, permissions, domain ports already injected on `plugin.Bootstrap`.
2. Keep Shopanda core as the implementation; do not leak infrastructure SDKs into the public surface.
3. Support **dual-mode** for one major version: in-tree plugins continue to work; external modules may depend on the public module (published tag or `replace` during transition).
4. Compatibility policy: semantic import path + documented deprecation window before removing any public symbol.
5. Migration guide: product fork → dependency-based product repo (E3) only after E1–E2 are green.

## Consequences

- Positive: product repos can stop carrying full Shopanda history; upgrades become module bumps.
- Negative: large boundary design; risk of freezing the wrong API. Mitigation: land A–D first, then ADR review (PR-1540) before moving packages.
- Negative: GPL obligations still apply to combined distributions that link the engine.

## Implementation slices (later)

| Slice | Intent |
| --- | --- |
| E0 / PR-1540 | This ADR accepted; inventory of `internal/` imports used by `plugins/example` + B2B + external demos |
| E1 | Move/extend ports onto the public SDK; core adapts |
| E2 | Dual-mode docs + CI for an external-module fixture |
| E3 | Migration guide: fork → module dependency |
| E4 | Products (e.g. Metal Dogs) may drop full-tree fork if desired |

## Out of scope for E0

- Implementing the move in the same PR as the ADR.
- `.so` loading or a plugin marketplace.
