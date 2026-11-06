# Spec: Product packaging

Status: accepted for Tracks A–D (Phase 17). Track E (public SDK) is specified separately in [`PUBLIC_EXTENSION_SDK.md`](PUBLIC_EXTENSION_SDK.md).

## Problem

Shopanda is both an **open-source platform repository** and the **distribution** that product sites start from. Those roles conflict when:

- platform CI assumes Shopanda product-doc paths;
- release publishes `ghcr.io/<owner>/shopanda`;
- authors replace `docs/` and break fail-closed jobs;
- the only documented extension path is “edit this repo,” not “own a product remote.”

## Goals

1. Platform development on `akarso/shopanda` keeps required CI.
2. Product consumers can develop and deploy with **no** vendor CI and **no** git hooks.
3. Opt-in sample GitHub and GitLab pipelines call the same portable scripts.
4. `product-init` creates a Phase-13-correct external plugin skeleton.
5. Docs name one job-type scheme: `integration.sync.{slug}.{name}`.

## Non-goals

- Removing upstream CI.
- Dynamic plugin loading.
- Shipping Metal Dogs–specific code in this repository.
- Completing the public SDK in Tracks A–D (design only in Track E).

## Consumer modes

| Mode | Remotes | CI | Typical use |
| --- | --- | --- | --- |
| **1. Plugin-in-upstream** | Work on `origin` = shopanda | Live `.github/workflows` | Contributing demos / patches to Shopanda |
| **2. Product fork** | `origin` = product; `upstream` = shopanda (**fetch-only**) | None by default; optional `ci-samples/` | Real merchant / brand site |
| **3. Local-only** | Any; may never push | None | FTP / rsync / manual binary copy |

Mode 2 is the primary product path after Phase 17. Mode 1 remains valid for Shopanda contributors. Mode 3 is first-class: omit samples, never add hooks.

## Ownership split

### Platform-owned (must retain or replace with an equivalent contract)

| Artifact | Role |
| --- | --- |
| `scripts/ci/*` | Portable quality checks |
| `docs/GOVULN_BASELINE.md` | Fail-closed vuln allowlist **if** you run govuln |
| `cmd/api/register_plugins.go` | Compile-time registration line for your plugin |
| Go module path `github.com/akarso/shopanda` | Required until Track E ships a public SDK |

### Product-owned (safe to replace)

| Artifact | Role |
| --- | --- |
| `README.md` (banner / remotes) | Product identity |
| `docs/guides/*` narrative beyond retained stubs | Product docs |
| `plugins/<slug>/` | Product plugin |
| `storefront/` (if any) | Product UI |
| `ci-samples/` copies under `.github/` or `.gitlab-ci.yml` | Only if opted in |

### Platform-only (do not copy as live product CI)

| Artifact | Role |
| --- | --- |
| `.github/workflows/ci.yml` | Shopanda upstream required checks |
| `.github/workflows/release.yml` | Shopanda upstream image publish |
| `scripts/promote-dev-to-main.sh` | Shopanda docs promote model |

Products that need CI **copy from `ci-samples/`** and adapt; they do not inherit upstream workflows by merge accident without review.

## Portable contract

| Script | Responsibility |
| --- | --- |
| `scripts/ci/unit.sh` | `go mod verify`, `gofmt`, `go vet`, `go test ./...` |
| `scripts/ci/integration.sh` | DSN-gated integration suite |
| `scripts/ci/govuln.sh` | Fail-closed govulncheck + baseline |
| `scripts/ci/docs-policy.sh` | Product-doc link policy + promote fixture (platform) |

Human entrypoint: `make check` → unit script. No git hooks in the default flow.

## Image naming

Release adapters resolve:

```text
ghcr.io/<owner>/${SHOPANDA_IMAGE_NAME:-shopanda}
```

Product forks that enable the sample release workflow **must** set `SHOPANDA_IMAGE_NAME` to their product slug (e.g. `metal_dogs`).

## Remotes (mode 2)

```bash
git remote -v
# origin    → product repository (push here)
# upstream  → https://github.com/akarso/shopanda.git  (fetch upgrades only)

git remote set-url --push upstream DISABLED
```

Never merge from a local Shopanda working tree. Always `git fetch upstream` from GitHub.

## Job types

Canonical only:

```text
integration.sync.{slug}.{name}
```

Register with `pluginsdk` `RegisterManual` / `RegisterOnEvent` / `RegisterCron` and enqueue with `Enqueue`. Free-form types such as `myproduct.generate` are **out of scope** until a future phase deliberately adds a generic job registry (not Phase 17).

## Scaffold

```bash
bash scripts/product-init.sh --slug myproduct
bash scripts/product-init.sh --slug myproduct --with-ci-samples
```

Creates `plugins/myproduct/` (health route, Migrator stub, README) plus `PRODUCT_README.myproduct.md` (optional root banner) via **same-filesystem stage-then-rename** under `.product-init-stage/` (failed renders leave no wreckage; no cross-device `TMPDIR` install). Prints the `register_plugins.go` snippet and config flag.

`--with-ci-samples` restores `scripts/ci/` and `ci-samples/` from `scripts/product-init/seed/` and writes a GOVULN stub when missing. It does **not** install live `.github/workflows`. If live trees differ from seed, it **refuses** unless `--force-ci-samples`. Before replace it backs up live trees under `.product-init-backup/` (including the already-in-sync path).

**Source of truth:** live `scripts/ci/` + `ci-samples/`. Seed is derived via `bash scripts/product-init/sync-seed.sh` (also refreshes `reserved-slugs.txt` from `plugins/*/`). CI runs `sync-seed.sh --check` inside `scripts/ci/unit.sh`.

First-party plugin slugs (every `plugins/<dir>` with `.go` files on the Shopanda tree) are **reserved** via `reserved-slugs.txt` and refused even with `--force` (CRLF-safe). Go keywords (`chan`, `type`, …) are refused. `--name` is charset-validated. Requires **python3**.

## Acceptance

See phase [`ROADMAP.md`](../ROADMAP.md) site-ready gate and validation target.
