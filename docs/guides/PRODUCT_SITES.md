# Building a product on Shopanda

Primary path for a **merchant / brand site** that owns its own git remote and
tracks Shopanda as `upstream`. For contributing demos inside this repository,
see [Create an External Plugin](DEVELOPER.md#create-an-external-plugin).

## CI philosophy

Quality checks (`scripts/ci/*`) are part of the platform. Vendor CI/CD YAML is
**not** part of the product runtime.

| Mode | Remotes | CI |
| --- | --- | --- |
| Plugin-in-upstream | `origin` = shopanda | Live `.github/workflows` on this repo |
| Product fork | `origin` = product; `upstream` = shopanda (**fetch-only**) | None by default; optional [`ci-samples/`](../../ci-samples/README.md) |
| Local-only / FTP | Any | None — no hooks, no pipelines |

Shopanda upstream keeps required GitHub Actions for engine development. Product
sites may omit all pipelines forever.

## Scaffold

From a Shopanda tree (clone or product fork):

Requires **python3** on PATH (template rendering).

```bash
bash scripts/product-init.sh --slug myproduct
bash scripts/product-init.sh --slug myproduct --with-ci-samples
# restores scripts/ci/ + ci-samples/ from seed + GOVULN stub; does NOT install live workflows
# If you customized those trees, the flag refuses unless you pass --force-ci-samples
# (writes a timestamped backup under .product-init-backup/).
```

On a Shopanda checkout, refresh the seed after editing `scripts/ci/` or `ci-samples/`:

```bash
make sync-seed   # or: bash scripts/product-init/sync-seed.sh
```

This creates `plugins/myproduct/` with:

- `GET /api/myproduct/health`
- `plugin.Migrator` + placeholder migration
- README with registration and job-type notes
- `PRODUCT_README.myproduct.md` — optional root README banner (does not overwrite `README.md`)

Register in `cmd/api/register_products.go` (product-owned; do not edit
`register_plugins.go`), rebuild, hit the health route.
Slugs that are Go keywords (`chan`, `type`, …) or first-party plugin names are refused.

### Product plugin config (no typed core field)

Unknown keys under `plugins:` are kept in `PluginsConfig.Extra` and flattened as
`plugins.<slug>.*` (including `metaldogs` — no typed core field). Nested YAML
maps become dotted keys (`generation.provider`). Env overlay uses underscores
(`SHOPANDA_PLUGINS_<SLUG>_GENERATION_PROVIDER`). Both forms are **stored** as
exact map keys (aliases stay in sync); Host.Config* does exact lookup — do not
assume a runtime `.`↔`_` rewrite. Conflicting flat vs nested keys that alias
to the same path (`my_key` vs `my.key`) fail Load. Env↔env conflicts need two
env names that alias under `.`≡`_` (e.g. `…_MY_KEY` and `…_MY.KEY` with
different values); normal underscore-only overlays do not hit that path. Empty
env values are skipped (empty = unset; same as typed plugin envs) so they do
not clobber YAML. Lists under a product slug are rejected. Use a slug without
underscores in the env form, or set values in YAML. Do not add a typed struct
field in `internal/platform/config` for product plugins.

Public routes with `pluginsdk.NoCache()` denylist the longest static path
before the first `{` (prefix match, so siblings under that prefix also get
no-store). A `{` cut that leaves an empty or one-segment prefix is rejected at
registration (`/{id}`, `/api/{v}/…`); known broad roots (`/api`) are refused
even without braces. A fully static one-segment mount (`GET /customizer`) is
allowed.

## Portable checks (no CI required)

```bash
make check                 # == bash scripts/ci/unit.sh (includes Phase 17 script tests when present)
bash scripts/ci/integration.sh   # needs SHOPANDA_TEST_DSN
bash scripts/ci/govuln.sh        # needs govulncheck on PATH
bash scripts/ci/docs-policy.sh   # product trees: leave SHOPANDA_PLATFORM unset
# make docs-policy sets SHOPANDA_PLATFORM=1 (Shopanda upstream / promote fixture required)
```

`SHOPANDA_PLATFORM=1` is set only by Shopanda’s live GitHub Actions (unit + docs-policy), not by `unit.sh` auto-detect. A normal clone can `product-init` then `make check` without putting your slug on the reserved list.

Optional local pre-push (never installed by default):

```bash
# .git/hooks/pre-push — only if you want it
#!/bin/sh
exec make check
```

## Sample CI

See [`ci-samples/README.md`](../../ci-samples/README.md) and
[`../runbooks/product-ci-samples.md`](../runbooks/product-ci-samples.md).

Release samples refuse to publish as `ghcr.io/<owner>/shopanda`. Set repository
variable `SHOPANDA_IMAGE_NAME` to your product slug.

## Remotes

```bash
git remote add upstream https://github.com/akarso/shopanda.git   # if missing
git remote set-url --push upstream DISABLED
git fetch upstream
```

Never merge from a local Shopanda working tree. Upgrade procedure:
[`../runbooks/product-fork-upgrade.md`](../runbooks/product-fork-upgrade.md).

## Job types

Canonical only:

```text
integration.sync.{slug}.{name}
```

Use `pluginsdk` `RegisterManual` / `RegisterOnEvent` / `RegisterCron` + `Enqueue`.
Free-form names (`myproduct.generate`) are out of scope.

Contract summary: [`PLUGIN_RUNTIME_CONTRACT.md`](PLUGIN_RUNTIME_CONTRACT.md).

## Docs ownership

| Keep / provide equivalent | Replace freely |
| --- | --- |
| `scripts/ci/*` if you run checks | Product README banner |
| `docs/GOVULN_BASELINE.md` if you run govuln | Narrative guides under `docs/` |
| Registration line in `register_products.go` | `plugins/<slug>/`, storefront |

Do not assume a blank `docs/` tree is safe if you still run sample govuln/docs-policy jobs.

## Product-owned convention

One place for “where does product stuff go?” Canonical list:
[`docs/PRODUCT_OWNED.md`](../PRODUCT_OWNED.md) (machine-readable manifest +
`scripts/ci/check-product-owned.sh`). Deviation docs must not invent a second
allowlist — read that manifest.

| Need | Put it here (owned) | Do **not** edit (upstream + hook) |
| --- | --- | --- |
| Make targets | `Makefile.product` | root `Makefile` (`-include Makefile.product`) |
| Extra CI checks | `product/checks/*.sh` | `scripts/ci/docs-policy.sh` (runs them) |
| Env examples | `.env.example.product` | `.env.example` |
| Config examples | `configs/config.example.product.yaml` | `configs/config.example.yaml` |
| Compose | `docker-compose.override.yml` | `docker-compose.yml` |
| Extra reserved slugs | `product/reserved-slugs.txt` | `scripts/product-init/reserved-slugs.txt` |
| Extra gitignores | `.gitignore.product` → `bash scripts/ci/apply-product-gitignore.sh` | `.gitignore` outside markers |
| Plugin registration | `cmd/api/register_products.go` | `cmd/api/register_plugins.go` |
| Plugin code | `plugins/<slug>/` | — |

`product-init` scaffolds `plugins/<slug>/`, optional `PRODUCT_README.<slug>.md`,
seeds `product/` when missing, and seeds `register_products.go` only if absent.
It must not dirty upstream-owned roots (asserted in `product-init_test.sh`).

### merge=ours (product repo)

Example `.gitattributes` in the **product** repo (not required upstream) — keep
in sync with the **owned** rows in `docs/PRODUCT_OWNED.md`:

```gitattributes
README.md merge=ours
AGENTS.md merge=ours
RUNBOOK.md merge=ours
PLUGINS.md merge=ours
Makefile.product merge=ours
cmd/api/register_products.go merge=ours
product/** merge=ours
docker-compose.override.yml merge=ours
.gitignore.product merge=ours
.env.example.product merge=ours
configs/config.example.product.yaml merge=ours
.github/workflows/** merge=ours
```

Engine/core paths and **hook** rows should **not** use `merge=ours` — take
upstream there. After an upstream bump of `.gitignore`, re-run
`bash scripts/ci/apply-product-gitignore.sh` so the marked fragment returns.

### Makefile.product

The shared root `Makefile` ends with `-include Makefile.product` (missing file
is a no-op). Put product-only targets in `Makefile.product` and keep them out of
the shared Makefile so bumps do not fight your custom recipes.

## Public SDK (later)

A public extension module so products can stop forking for `internal/` access is
Track E of Phase 17 (spec on the `dev` branch under
`docs/phase-17-product-packaging/specs/PUBLIC_EXTENSION_SDK.md`). Until then,
product forks remain the supported model.
