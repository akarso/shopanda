# Upstream deviations (template)

Copy into your product repository (e.g. `docs/UPSTREAM_DEVIATIONS.md`). List every
path that differs from the pinned Shopanda commit outside `plugins/<slug>/`,
product `storefront/`, and product-owned docs.

Pinned Shopanda commit: `<sha>`

| Path | Why it differs | Introduced |
| --- | --- | --- |
| `README.md` | Product banner and remotes | product-init |
| `cmd/api/register_plugins.go` | Register product plugin | product-init |
| `.gitignore` | Product-specific ignores | optional |

`scripts/ci/` and `ci-samples/` matching upstream are **not** deviations.
Custom live workflows under `.github/workflows/` **are** deviations — document them.
