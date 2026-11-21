# AGENTS.md

## Purpose

This document defines **global collaboration rules** for both human contributors and LLM agents working on this project.

These rules apply across the entire system (core, plugins, API, optional frontend).

---

## Core Principles

* Small, reviewable changes over large rewrites
* Explicit architecture over hidden magic
* Composition over inheritance
* Deterministic behavior over implicit side effects
* Core remains minimal and stable
* Complexity must be **opt-in (via plugins/modules)**

---

## Development Workflow

* Work is delivered via **small, focused PRs**
* One PR = one responsibility (strict)
* Each PR must be:

  * runnable
  * testable (when applicable)
  * reviewable in ~10–20 minutes

### Quality checks (portable)

* Prefer `make check` / `bash scripts/ci/unit.sh` before opening a PR (same contract as `CI / unit`).
* Vendor CI YAML is an adapter for this upstream repo; product sites use [`ci-samples/`](ci-samples/) opt-in or no CI. Do not add mandatory git hooks.
* Product forks: see [`docs/guides/PRODUCT_SITES.md`](docs/guides/PRODUCT_SITES.md) and Phase 17 on `dev`.

### PR Guidelines

* Avoid “big bang” implementations
* Prefer vertical slices (HTTP → application → domain)
* Do not introduce unused abstractions
* Do not pre-build for hypothetical use cases

---

## Architecture Rules (Global)

### Style

* Hexagonal architecture (ports & adapters)
* Domain-first design
* No framework-driven structure

---

### Dependency Direction

```text
interfaces → application → domain
                ↓
         infrastructure
```

### Rules

* Domain layer:

  * no database access
  * no HTTP logic
  * no infrastructure dependencies

* Application layer:

  * orchestrates use cases
  * coordinates domain + infrastructure

* Infrastructure:

  * implements interfaces (DB, cache, plugins, etc.)

---

## Extensibility Model (CRITICAL)

The system is extended via **explicit mechanisms only**:

### 1. Events

* async or sync reactions
* no ordering guarantees

### 2. Pipelines

* deterministic transformations (e.g. pricing)

### 3. Workflows

* ordered, stateful flows (e.g. checkout)

### 4. Composition Pipelines

* API response building (PDP, PLP)

---

### Rules

* Do NOT override core logic directly
* Do NOT modify core behavior implicitly
* All extensions must go through:

  * events
  * hooks
  * pipelines
  * workflows

---

## Plugin Philosophy

* Plugins provide **optional complexity**
* Core defines contracts; plugins provide implementations

### Examples:

* search (postgres / meilisearch)
* cache (none / redis)
* API (REST / GraphQL)
* frontend (none / custom)

---

### Plugin Rules

* Plugins must be isolated
* Plugins must not mutate core schema
* Plugins must not introduce hidden side effects
* Plugins must be explicit in registration and behavior

---

## Testing Rules

* Tests are encouraged, but **not required in early PRs**
* Focus on:

  * correctness
  * determinism
  * reproducibility

### Later stages:

* Domain → unit tests
* Application → flow tests
* Integration → API + DB

---

## Code Quality

* Prefer simple, readable code over abstraction
* Avoid deep nesting and long functions
* One responsibility per function/module
* No “utils dumping ground”

---

## Go-Specific Guidelines

* Prefer explicit wiring over DI frameworks
* Use interfaces at boundaries only
* Avoid unnecessary abstractions
* Keep packages small and focused

---

## Communication Rules for LLMs

* If uncertain → ask
* Do not hallucinate APIs or structures
* Follow existing project structure strictly
* Do not introduce scope creep
* Do not “optimize” prematurely

---

## Anti-Patterns (Strictly Forbidden)

* ❌ Core overrides (Magento-style)
* ❌ Hidden magic / reflection-based behavior
* ❌ Global state mutation
* ❌ Large, multi-concept PRs
* ❌ Premature generalization

---

## Repository-Specific Rules

Additional documents:

* `BACKEND.md` — backend-specific modules & sequencing
* `FRONTEND.md` — frontend strategy (optional/headless)
* `PLUGINS.md` — plugin authoring guide

## Documentation

**First: which branch does this PR target?**

- **`main` (hotfix or promote):** product docs only. Do **not** create or edit `docs/phase-*`.
- **`dev` (default for features):** PR spec + phase index/roadmap + product docs as applicable.

Product docs (guides, current C4, runbook, `docs/README.md`) ship on **`main`**. Working history (`docs/phase-*` — PR specs, phase roadmaps, historical specs) lives on **`dev` only**. See [docs/README.md](docs/README.md). Never raw-merge `dev` into `main`; use `scripts/promote-dev-to-main.sh`.

Every **finished PR** must ship code **and** updated docs in the same change set.

### PRs targeting `dev`

Do not mark a PR done until the checklist below is complete (or each item is explicitly N/A and noted in the PR spec).

**Checklist** (see also `.cursor/rules/pr-documentation-checklist.mdc`):

| Artifact | When to update |
| --- | --- |
| **PR spec** | Always — `docs/phase-<n>-<name>/prs/PR-<id>.md` (summary, scope, validation, what changed) |
| **Phase index** | Always — `prs/README.md` status → `done` + link |
| **Phase roadmap** | When PR status or recommended order changes — `ROADMAP.md` |
| **Phase README** | When track completion or “next PR” guidance changes |
| **C4 diagrams** | When adding/removing/changing components, containers, ports, or integrations — `docs/diagrams/c4-{context,container,component,code}.md` |
| **PLUGINS.md** | New plugin ports, reference plugins, registration or config patterns |
| **DEVELOPER.md** / **PLUGIN_COMPOSITION.md** | New integrator APIs, hooks, pipelines, workflows, extension points |
| **DEPLOYMENT.md** | New env vars, config keys, runtime modes, deploy/ops steps |
| **RUNBOOK.md** | Operator-facing behavior, troubleshooting, plugin/config notes |
| **config.example.yaml** | New `plugins.*` or top-level config keys |
| **Root README.md** | New CLI commands, major capabilities, planning links |
| **Plugin README** | Reference/demo plugins under `plugins/<name>/README.md` |

Rules:

* PR spec path follows the **active phase** (e.g. phase-9 for PR-862+), not legacy `/docs/prs/`.
* Update C4 only when architecture changed; say “no C4 change” in the PR spec if unchanged.
* Config additions need **yaml example**, **env override** (if applicable), and **flatten map** in `internal/platform/config` when the project uses it.
* Validation section in the PR spec must list **tests run** and **build commands**.
* Living operator/CI artifacts belong in `docs/guides/`, `docs/diagrams/`, or `docs/GOVULN_BASELINE.md` — never nest them under `docs/phase-*`.

### PRs targeting `main`

Hotfixes and promote PRs update **product docs only** (guides, C4, runbook, README, PLUGINS.md, `docs/README.md`). They must **not** add `docs/phase-*`. `CI / docs-main` fails if those paths are present; require that check on `main` only, never on `dev`.

---

## Extension recipes

Short pointers for plugin work (full mechanism choice → [Extension Points](docs/guides/EXTENSION_POINTS.md)).

| Goal | Recipe |
| --- | --- |
| Pick hooks vs pipeline vs events | [EXTENSION_POINTS.md — Quick catalog](docs/guides/EXTENSION_POINTS.md#quick-catalog--i-want--use) |
| Tutorial walkthrough | [DEVELOPER.md](docs/guides/DEVELOPER.md) |
| Multi-plugin ordering on the same seam | [PLUGIN_COMPOSITION.md](docs/guides/PLUGIN_COMPOSITION.md) |
| Public SDK plugin (`pkg/pluginsdk` only) | Register `pluginsdk.Adapt(yours)`; copy [`plugins/extref`](plugins/extref/README.md) |
| Plugin-only cart field + trusted add | `RegisterField` with `PluginOnlyWrite`, then `WithPluginWrite` + `CartPort.AddItem` (pitfalls in EXTENSION_POINTS) |
| Session cart from a public route | `CartPort.EnsureStorefrontCart` (`pkg/storefrontsession`); keep `cart_id` out of JSON — see `plugins/extref` design-line |
| Failed first guest add left empty cart | `CartPort.DiscardEmptyGuestCart(ctx, w, r, cart.ID)` with Ensure id (clears cookie only if deleted) |
| Validate design/extensions during AddItem | Effective `CartView` line overlays pending codes; `pending_line` only when AddItem has extensions |
| Signed-in customer + owner-token on plugin route | Keep `AuthMiddleware` for opaque (non-JWT) owner tokens; JWT-shaped owners still 401 until confirmed; `pluginsdk.CustomerID(r)` |
| Variant → product slug in hooks | `extapi.ClassifyProductSlug` / `FindVariants` — Match validate; Other skip; Unknown fail closed |
| Capability ports | `Needs()` + `Require(CapCart)`; ensure serve/CLI bind ports (Track E PR-1547) |
| Read `queue.driver` (or search/cache/media) at Init | `Host.PlatformConfig` / `pluginsdk.RequireQueueDriver` — not `ConfigValue` |
| Request id for log correlation | `pluginsdk.RequestIDFromContext(ctx)` — not `internal/platform/requestctx` |
| Human price → minor units | `extapi.ParseMinorUnits(s, exponent)` + `extapi.IsValidCurrency` (caller passes exponent) |
| Job get / counts / requeue | `Need(CapJobs)` + `Host.Jobs()` (`Get`, `CountsByStatus`, `CountsByType`, `Requeue`) — not `internal/domain/jobs` / `BoundQueue`. Plugin CLI skips CapJobs/CapMedia assert (ports stay unbound there). |
| Idempotent sync enqueue | `SyncJobs.Enqueue(…, pluginsdk.WithJobID(uuid))` with `JobsPort` bound; Enqueue resolves Conflict (pending=no-op, terminal=Requeue with one WrongAttempt retry / NotTerminal=success, second WrongAttempt hard-fails, wrong type=error). Second payload discarded. |
| Media storage wrap/serve | `Need(CapMedia)` + `Host.Media()` — not `internal/domain/media` |
| Isolated Postgres in plugin tests | `harness.OpenIsolated` (+ `AdaptInit` `Options.DB` for `BootstrapDB`) |
| Drop `plugin.App` import | Host + `Adapt` / `NewHost`; sync via `SDK.Integration`; map in PLUGINS.md |
| Seed CLI + non-DB sibling | Plugin `Need(CapCatalogSeed)` + `CLICommand.Needs: CapCatalogSeed` on seed only; keep other commands `RequiresDB: false`. Empty Needs on another RequiresDB still over-binds the write port (compat; no Needs opt-out). |

---

## Guiding Principle

> Build a system where complex things are possible,
> without making simple things hard.

Clarity beats cleverness. Small steps beat big rewrites.
