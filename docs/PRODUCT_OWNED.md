# Product-owned paths

Single convention for files **outside** `plugins/<slug>/` that a product fork
may own or extend. Full prose: [guides/PRODUCT_SITES.md](guides/PRODUCT_SITES.md#product-owned-convention).

## Rule

| Kind | Meaning |
| --- | --- |
| **owned** | Product copy wins on bump (`merge=ours` in the product `.gitattributes`). Do not fork upstream to hang product behaviour. |
| **hook** | Upstream-owned file with real machinery (include, markers, script call). Products extend via the companion path in the note; leave the upstream file alone except for documented markers. |
| **companion** | Optional product sibling next to an upstream *example* file. Humans copy values into `.env` / `config.yaml` — there is **no** loader merge. Not CI machinery. |

`scripts/ci/check-product-owned.sh` (via `docs-policy`) reads the fenced
manifest below. Product deviation docs should not invent a second allowlist —
point at this file.

Coverage note: this convention covers Make / docs-policy checks / compose /
gitignore / reserved-slugs / registration. Metal Dogs still edits some other
roots (e.g. `.github/scripts/*`, `.cursor/rules/*`) — those remain follow-ups,
not claimed done by the hook table.

## Manifest (machine-readable)

Tab-separated. Columns: `path`, `kind`, `expect`, `note`.

`expect` for **hook** rows (checked by `check-product-owned.sh`):

| expect | Meaning |
| --- | --- |
| `contains:SUB` | file must contain substring `SUB` |
| `contains_all:A\|B` | file must contain each `|`-separated substring |
| `gitignore_markers` | exactly one ordered `>>>` / `<<<` shopanda-product-gitignore pair |
| `exists` | file must exist |

**owned** / **companion** rows use `-` for expect (existence optional).

```
# path	kind	expect	note
cmd/api/register_products.go	owned	-	plugin registration
Makefile.product	owned	-	Make targets; root Makefile -include
product/	owned	-	overlay tree (checks, reserved-slugs, README)
docker-compose.override.yml	owned	-	Compose auto-loads beside docker-compose.yml
.gitignore.product	owned	-	fragment synced into .gitignore markers
.env.example.product	companion	-	copy manually into .env (no loader merge)
configs/config.example.product.yaml	companion	-	copy manually into config.yaml (no loader merge)
PRODUCT_README.*	owned	-	optional root README banners from product-init
README.md	owned	-	product narrative (optional merge=ours)
AGENTS.md	owned	-	product agent rules (optional merge=ours)
RUNBOOK.md	owned	-	product runbook (optional merge=ours)
PLUGINS.md	owned	-	product plugin index (optional merge=ours)
.github/workflows/**	owned	-	live vendor CI if the product chooses any
Makefile	hook	contains:-include Makefile.product	Make product include
scripts/ci/docs-policy.sh	hook	contains_all:check-product-owned.sh|product/checks	runs product checks + owned check
.gitignore	hook	gitignore_markers	markers for .gitignore.product
docker-compose.yml	hook	contains:docker-compose.override.yml	Compose loads override.yml
scripts/product-init.sh	hook	contains:product/reserved-slugs.txt	reads product reserved overlay
scripts/product-init/reserved-slugs.txt	hook	exists	upstream first-party reserved list
```
