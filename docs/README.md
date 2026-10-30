# Documentation

Shopanda splits **product docs** from **working history**. Git will not keep a folder on `dev` only unless promotion strips it — see `scripts/promote-dev-to-main.sh`.

## Product docs (ship on `main`)

These must stay complete on a clone of `main`. A default-branch checkout has to be operable and understandable.

| Path | Audience |
| --- | --- |
| [README.md](../README.md), [PLUGINS.md](../PLUGINS.md), [RUNBOOK.md](../RUNBOOK.md) | Operators, integrators, plugin authors |
| [guides/](guides/) | Merchant, deployment, developer, theme, headless, extension |
| [diagrams/](diagrams/) | Current C4 (context → container → component → code) |
| [COMMERCIAL.md](COMMERCIAL.md) | Open core vs paid B2B |
| [GOVULN_BASELINE.md](GOVULN_BASELINE.md) | CI fail-closed vuln exceptions |

Do **not** nest living operator or CI artifacts under `docs/phase-*`.

## Working history (`dev` only)

Phase folders, PR specs, in-progress roadmaps, and historical design dumps live under `docs/phase-*` on the [`dev` branch](https://github.com/akarso/shopanda/tree/dev/docs).

Browse current notes: [`tree/dev/docs`](https://github.com/akarso/shopanda/tree/dev/docs).

PRs targeting `dev` still write a PR spec and update the phase index/roadmap. PRs targeting `main` (hotfixes and promotes) update product docs only — they must not add `docs/phase-*`. CI fails if those paths are present on `main`.

`dev` → `main` is never a raw merge. Use `scripts/promote-dev-to-main.sh` so working history is stripped before the promote PR.

## Guides

- [Merchant Guide](guides/MERCHANT.md)
- [Deployment Guide](guides/DEPLOYMENT.md)
- [Developer Guide](guides/DEVELOPER.md)
- [Runtime Modes](guides/RUNTIME_MODES.md)
- [EU Compliance](guides/COMPLIANCE_EU.md)
- [Integrator Platform](guides/INTEGRATOR_PLATFORM.md)
- [Theme author guide](guides/THEME_SLOTS.md)
- [Headless storefront](guides/HEADLESS.md)
- [Extension Points](guides/EXTENSION_POINTS.md)
- [Plugin Composition](guides/PLUGIN_COMPOSITION.md)
- [Extension API](guides/EXTENSION_API.md)
