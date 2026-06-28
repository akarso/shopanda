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
- Does not modify core schema directly; plugin-owned tables ship via embedded migrations

Feature map: [Phase 5 roadmap — B2B PRs](../../docs/phase-5-maturity/ROADMAP.md).

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
