# Upstream deviations (template)

Copy into your product repository (e.g. `docs/UPSTREAM_DEVIATIONS.md`).

**Product-owned paths** are listed once in upstream
[`docs/PRODUCT_OWNED.md`](../PRODUCT_OWNED.md) (owned vs hook). Do not maintain a
second allowlist. List here only:

1. Paths that differ from the pinned Shopanda commit **and** are not covered by
   an **owned** row / hook companion in that manifest, or
2. Owned paths whose *reason* for differing needs a human note.

Pinned Shopanda commit: `<sha>`

| Path | Why it differs | Introduced |
| --- | --- | --- |
| `README.md` | Product banner and remotes | product-init |
| `cmd/api/register_products.go` | Register product plugin | product-init |
| `.gitignore.product` | Product-specific ignores (synced via apply-product-gitignore) | optional |

`scripts/ci/` and `ci-samples/` matching upstream are **not** deviations.
Custom live workflows under `.github/workflows/` **are** (owned) — document them.
Editing a **hook** file (Makefile, `docker-compose.yml`, `.gitignore`, …) for
product behaviour is a process bug; move the change to the owned companion path.
Env/config **companion** fragments are copy-paste only — not loader merges.
