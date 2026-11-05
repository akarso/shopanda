# Shopanda B2B Module (Commercial)

Optional commercial extension for wholesale and business-buyer workflows.

**License:** Commercial — see [LICENSE](LICENSE) and [docs/COMMERCIAL.md](../../docs/COMMERCIAL.md).  
**Open core:** Everything outside this directory remains [GPL v3](../../LICENSE).

---

## Status

**Customer groups (PR-500)** and **group-aware pricing (PR-501)** — domain ports in open core; schema, repository, pricing step, and admin API in this plugin. Quotes, PO, and approvals remain planned.

---

## Enable (development)

```yaml
plugins:
  b2b:
    enabled: true
    license_key: "DEV-local"
```

Or via environment:

```env
SHOPANDA_PLUGINS_B2B_ENABLED=true
SHOPANDA_PLUGINS_B2B_LICENSE_KEY=DEV-local
```

Restart after changing config. Registration is in `cmd/api/register_plugins.go`.

### License keys (stub)

| Key pattern | Valid? | Use |
| --- | --- | --- |
| *(empty)* | No | B2B plugin fails init with a clear message |
| `DEV-*` | Yes | Local development and CI experiments |
| Other | No | Until online validation ships |

Production keys will be issued on purchase. Do not rely on `DEV-*` outside development.

---

## Customer groups API

Requires B2B plugin enabled and admin JWT with `b2b.groups.read` / `b2b.groups.write`.

| Method | Path | Permission |
| --- | --- | --- |
| GET | `/api/v1/admin/customer-groups` | `b2b.groups.read` |
| POST | `/api/v1/admin/customer-groups` | `b2b.groups.write` |
| GET | `/api/v1/admin/customer-groups/{groupId}` | `b2b.groups.read` |
| PUT | `/api/v1/admin/customer-groups/{groupId}` | `b2b.groups.write` |
| DELETE | `/api/v1/admin/customer-groups/{groupId}` | `b2b.groups.write` |
| GET | `/api/v1/admin/customers/{customerId}/customer-group` | `b2b.groups.read` |
| PUT | `/api/v1/admin/customers/{customerId}/customer-group` | `b2b.groups.write` |
| DELETE | `/api/v1/admin/customers/{customerId}/customer-group` | `b2b.groups.write` |

### Group prices

Requires admin currency context (same as variant price admin).

| Method | Path | Permission |
| --- | --- | --- |
| GET | `/api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price` | `b2b.prices.read` |
| PUT | `/api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price` | `b2b.prices.write` |
| DELETE | `/api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price` | `b2b.prices.write` |

---

## Architecture

B2B extends Shopanda through the same `plugin.Plugin` contract as external plugins:

- Implements domain **ports** defined in open core (never the reverse)
- Registers admin permissions, HTTP routes, pricing/checkout steps via `plugin.App`
- Does not modify core schema directly; plugin-owned tables ship via embedded migrations under `migrations/`

### Schema migrations (PR-1085)

B2B implements `plugin.Migrator` once (`Migration()` returns the embed FS + adopt options). `app migrate` / `setup` apply that FS after core migrations **without** calling `Init` (and without a license check — schema is separate from enabling the commercial module). `InitAll` applies the same spec before `Init` when `Bootstrap.DB` is set; a Migrator failure fails `serve` (and worker/scheduler). Direct `Init` without prior migrate/`InitAll` does not create tables. Versions are `plugin:shopanda/b2b:{basename}`. Each file uses its own transaction; the advisory lock key is per plugin (released between files so 003/004 lock-splitting still works).

**Legacy adopt:** only the frozen pre-1085 set (`001`–`004`) may be remapped from bare basename rows. Later files never consult bare keys. Fresh applies of `001`–`004` also dual-write the bare key (those basenames are reserved in global `schema_migrations`) so rolling the binary back to pre-1085 `RunFS` still skips those files. Rolling back a release does not undo that release’s schema. Do not rename existing `migrations/*.sql` files — that would desync tracking.

**Reset:** delete both `plugin:shopanda/b2b:*` and the bare `001`–`004` rows (and drop the B2B tables if you intend to re-run DDL). Deleting only namespaced rows re-adopts from bare and skips SQL.

**Boot lock:** `Init` waits up to 5 minutes **per file for the advisory lock only** (`migrate.DefaultPluginLockWait`); DDL itself is not wall-clock capped. `ErrMigrationLockHeld` fails Init — a peer restart during a long first-boot migration is expected while the holder finishes; see RUNBOOK.

Customer groups and group-aware pricing ship in this plugin. Quotes, PO, and approvals remain planned. B2B PR tags live on the `dev` branch under `docs/phase-5-maturity/ROADMAP.md`.

---

## Package layout

```text
plugins/b2b/
  plugin.go       # Plugin entry, Init wiring
  license.go      # Entitlement validation (stub → online check)
  migrations/     # customer_groups, customer_group_members
  groups/         # Postgres repo + admin HTTP handlers
  pricing/        # Group price repo, pipeline step, admin HTTP
  LICENSE         # Commercial terms (draft)
  README.md
  # Future:
  # quotes/       # backlog
  # company/      # multi-buyer accounts
```

---

## See also

- [Commercial licensing](../../docs/COMMERCIAL.md)
- [Developer guide — three tiers](../../docs/guides/DEVELOPER.md#three-tier-extension-model)
- [Example external plugin](../example/README.md) — OSS reference for plugin mechanics
