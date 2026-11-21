# Product-owned paths

Single convention for files **outside** `plugins/<slug>/` that a product fork
may own or extend. Full prose: [guides/PRODUCT_SITES.md](guides/PRODUCT_SITES.md#product-owned-convention).

## Rule

| Kind | Meaning |
| --- | --- |
| **owned** | Product copy wins on bump (`merge=ours` in the product `.gitattributes`). Do not fork upstream to hang product behaviour. |
| **hook** | Upstream-owned file. Products extend via the named companion path; leave the upstream file alone except for documented markers. |

`scripts/ci/check-product-owned.sh` (via `docs-policy`) reads the fenced
manifest below. Product deviation docs should not invent a second allowlist —
point at this file.

## Manifest (machine-readable)

```
# path	kind	note
cmd/api/register_products.go	owned	plugin registration hook
Makefile.product	owned	Make targets; root Makefile -include
product/	owned	overlay tree (checks, reserved-slugs, README)
docker-compose.override.yml	owned	Compose auto-loads beside docker-compose.yml
.gitignore.product	owned	fragment synced into .gitignore markers
.env.example.product	owned	env example fragment (docs + copy into .env)
configs/config.example.product.yaml	owned	config example fragment (docs + copy into config.yaml)
PRODUCT_README.*	owned	optional root README banners from product-init
README.md	owned	product narrative (optional merge=ours)
AGENTS.md	owned	product agent rules (optional merge=ours)
RUNBOOK.md	owned	product runbook (optional merge=ours)
PLUGINS.md	owned	product plugin index (optional merge=ours)
.github/workflows/**	owned	live vendor CI if the product chooses any
Makefile	hook	-include Makefile.product
scripts/ci/docs-policy.sh	hook	runs product/checks/*.sh + check-product-owned
.gitignore	hook	markers for .gitignore.product
.env.example	hook	points at .env.example.product
configs/config.example.yaml	hook	points at configs/config.example.product.yaml
docker-compose.yml	hook	Compose loads docker-compose.override.yml
scripts/product-init/reserved-slugs.txt	hook	plus product/reserved-slugs.txt
```
