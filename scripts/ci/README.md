# Portable CI contract

These scripts are the quality contract for Shopanda and for product sites.

| Script | Purpose |
| --- | --- |
| `unit.sh` | `go mod verify`, `gofmt`, `go vet`, `go test ./...`, then `go test -race ./internal/application/cart/` |
| `integration.sh` | DSN-gated integration suite |
| `govuln.sh` | Fail-closed govulncheck + baseline |
| `docs-policy.sh` | Product-doc links + product-owned checks + promote fixture |
| `check-product-docs-links.sh` | Link scan (skips missing roots) |
| `check-product-owned.sh` | `docs/PRODUCT_OWNED.md` manifest + hook markers |
| `apply-product-gitignore.sh` | Sync `.gitignore.product` into `.gitignore` markers |

Human entrypoint: `make check` from the repo root.

Shopanda upstream `.github/workflows` call these scripts. Product sites may
copy [`ci-samples/`](../../ci-samples/) or run scripts locally only — no
required vendor CI, no git hooks.
