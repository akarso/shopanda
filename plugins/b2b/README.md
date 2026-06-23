# Shopanda B2B Module (Commercial)

Optional commercial extension for wholesale and business-buyer workflows.

**License:** Commercial — see [LICENSE](LICENSE) and [docs/COMMERCIAL.md](../../docs/COMMERCIAL.md).  
**Open core:** Everything outside this directory remains [GPL v3](../../LICENSE).

---

## Status

**Scaffold only.** License validation stub is wired; B2B features (customer groups, quotes, PO, approvals) are planned in Phase 5 and tagged `[b2b]` in the roadmap.

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

## Architecture

B2B extends Shopanda through the same `plugin.Plugin` contract as external plugins:

- Implements domain **ports** defined in open core (never the reverse)
- Registers admin permissions, HTTP routes, pricing/checkout steps via `plugin.App`
- Does not modify `internal/domain` or core schema directly

Planned feature map: [Phase 5 roadmap — B2B PRs](../../docs/phase-5-maturity/ROADMAP.md).

---

## Package layout (intended)

```text
plugins/b2b/
  plugin.go       # Plugin entry, Init wiring
  license.go      # Entitlement validation (stub → online check)
  LICENSE         # Commercial terms (draft)
  README.md
  # Future:
  # groups/       # PR-500–501
  # quotes/       # backlog
  # company/      # multi-buyer accounts
```

---

## See also

- [Commercial licensing](../../docs/COMMERCIAL.md)
- [Developer guide — three tiers](../../docs/guides/DEVELOPER.md#three-tier-extension-model)
- [Example external plugin](../example/README.md) — OSS reference for plugin mechanics
