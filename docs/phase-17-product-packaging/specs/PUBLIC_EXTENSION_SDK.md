# Spec / ADR: Public extension SDK (Track E)

- **Status:** Accepted
- **Date:** 2026-11-06 (accepted 2026-11-16)
- **Related:** Phase 17 Track E, PR-1540+; Extension ergonomics backlog in [ROADMAP.md](../ROADMAP.md)
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
6. **Extension ergonomics** (Needs, catalog, reference plugin, pitfall fixes) ship as Track E follow-on PRs until product plugins need zero `internal/*` imports.

## Consequences

- Positive: product repos can stop carrying full Shopanda history; upgrades become module bumps.
- Negative: large boundary design; risk of freezing the wrong API. Mitigation: land A–D first, then ADR review (PR-1540) before moving packages.
- Negative: GPL obligations still apply to combined distributions that link the engine.

## Import inventory (PR-1540)

Prod `.go` files under `plugins/*` excluding `plugins/core` and `*_test.go` (counts = import statements).

### Top packages

| Count | Package | Role |
| --- | --- | --- |
| 19 | `internal/platform/plugin` | `Plugin` / `App` entrypoint |
| 6 | `internal/platform/logger` | sync/handlers |
| 6 | `internal/domain/customergroup` | b2b only |
| 5 | `internal/domain/pricing` | pricing step bodies |
| 4 | `internal/domain/shared` | `Money` |
| 3 | `internal/domain/tax` | taxdemo |
| 3 | `internal/domain/rbac` | permissions |
| 2 | `internal/domain/{mail,identity}` | maildemo / roles |
| 2 | `internal/platform/{apperror,httpx,id}` | b2b admin HTTP |
| 1 each | cart, catalog, customer, order, checkout, composition, migrate, config, cli | demos / b2b |

### Per plugin

| Plugin | Prod `internal/` | Why |
| --- | --- | --- |
| **example** | plugin, pricing, shared, order, logger, identity, rbac, config, cli | tour of surfaces |
| **b2b** | customergroup, rbac, plugin, apperror, httpx, id, shared, identity, migrate, customer, catalog, pricing | groups + admin + pricing step |
| **cartdemo** | plugin, pricing, shared, cart | handling fee + cart validate cast |
| **promodemo** | plugin only | already on `extapi` / `pluginsdk` |
| **pimdemo** | plugin, composition, logger | PDP composition step |
| **checkoutdemo** | plugin, checkout | checkout step |
| **taxdemo** | tax, pricing, plugin | tax calculator + pricing step |
| **slots / import / export** | plugin only | `extapi` |
| **integration / warehouse** | plugin, logger | sync via pluginsdk |
| **maildemo** | mail, plugin, logger | `RegisterMailSender` |
| **sitekit** | plugin | migrations |

### Gap vs `pkg/*` today

| Concern | Covered by `pkg/extapi` / `pluginsdk`? | Still needs `internal/` |
| --- | --- | --- |
| Hook / promo / import / export / sync registration | yes | cart payload cast → `domain/cart` |
| Pricing *position* helpers | yes | step body → `domain/pricing` + `shared` |
| Checkout *position* | yes | step body → `application/checkout` |
| Composition / PDP | no | `application/composition` |
| Tax / mail ports | register via App | implement domain ports |
| RBAC / Money / migrate / admin HTTP | no | platform + domain |
| Customer groups | no | b2b domain |

## Implementation slices

| Slice | Intent | Status |
| --- | --- | --- |
| E0 / PR-1540 | This ADR accepted; inventory above | **done** |
| E1+ | Host facade, public contracts, ports, Needs, pitfall fixes, docs, migrate demos | planned (see ROADMAP Track E / ergonomics) |
| E2 | Dual-mode docs + CI for an external-module fixture | later |
| E3 | Migration guide: fork → module dependency | later |
| E4 | Products may drop full-tree fork if desired | later |

## Out of scope for E0

- Implementing the move in the same PR as the ADR.
- `.so` loading or a plugin marketplace.
