# Phase 17 — Product packaging & consumer CI

Make Shopanda drop-in for **product sites** (separate product remotes that track this tree as `upstream`) without forcing GitHub Actions, Shopanda-shaped `docs/`, or a hard-coded GHCR image name onto every consumer.

## Why this phase

Phase 13 made plugin migrations and on-demand sync jobs site-ready. Phase 10 made **this** repository’s CI trustworthy. Starting a real product (fork + `plugins/<slug>/`) still fights the grain:

| Gap | Today | Impact |
| --- | --- | --- |
| Quality checks live only inside `.github/workflows` | Product forks inherit vendor YAML + fail when they replace `docs/` | CI blocks consumers who never opted into GitHub Actions |
| Release image hard-coded `shopanda` | Product tags publish into the platform GHCR name | Image collision across org repos |
| Bare `.gitignore` `src` | Ignores `storefront/src/**` | Silent loss of product storefront code |
| No product scaffold | Authors hand-copy `example` and guess job type names | Wrong free-form job names vs `integration.sync.{slug}.*` |
| Docs assume in-repo plugins only | Product-fork remotes / fetch-only upstream undocumented | Contaminated merges from local WIP clones |
| `internal/` imports force a fork forever | No public extension SDK path | Permanent fork tax |

## CI philosophy

> Quality checks are part of the platform. Vendor CI/CD files are not part of the product runtime. Shopanda keeps live GitHub Actions for its own development. Product sites get sample GitHub and GitLab pipelines plus a runbook; local-only and FTP-style deploys are first-class and require no pipeline and no hooks.

```text
scripts/ci/*     = portable contract (run locally or from any runner)
.github/workflows = Shopanda-upstream adapters (required on this repo)
ci-samples/       = copy-paste adapters for products (opt-in, never required)
```

Do **not** remove CI from `akarso/shopanda`. Do **not** install live workflows by default into product trees. Do **not** add mandatory git hooks.

## Design principles

- **One PR = one responsibility**, reviewable in ~10–20 minutes; each PR tagged **`[oss]`**.
- **Portable checks first** — YAML only shells out to `scripts/ci/*`.
- **Three consumer modes** — plugin-in-upstream, product fork, local-only / no CI (see [`specs/PRODUCT_PACKAGING.md`](specs/PRODUCT_PACKAGING.md)).
- **Scaffold over folklore** — `scripts/product-init.sh` creates a Phase-13-correct plugin skeleton.
- **SDK later** — Track E designs the public extension boundary; implement after A–D are site-ready.

See [`ROADMAP.md`](ROADMAP.md), [`specs/`](specs/), and [`prs/`](prs/).

## Status

**Tracks A–D done** (PR-1500–1534).
**Track E done** (PR-1540–1558): ADR, Host facade, public pricing/checkout/
composition/port contracts, declarative Needs, reference plugin + harness,
(PR-1552) all non-core plugins migrated to `pluginsdk.Host` so
`plugins/*` compiles against `pkg/*` only (`plugins/core/*` remains exempt
as the driver-adapter layer the SDK wraps), (PR-1553) CartPort session
cart + validate extension visibility, (PR-1554) optional customer identity
on plugin routes, (PR-1555) CatalogPort ProductSlug + FindVariants,
(PR-1556) `pending_line` on `cart.validate`, (PR-1557) `DiscardEmptyGuestCart`,
(PR-1558) `ProductVisible`. E2–E4 (external module publish + fork→module
guide) remain for later.

PR range **PR-1500–1558**.

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 10 | Platform CI / govuln / docs-policy | Shipped — keep for upstream |
| Phase 13 | Plugin Migrator + manual sync enqueue | Shipped — skeleton must match |
| Phase 15 / 16 | Merchant identity / visual refresh | Independent; do not block |
| Phase 17 | Product packaging + consumer CI + SDK ADR | This phase |
