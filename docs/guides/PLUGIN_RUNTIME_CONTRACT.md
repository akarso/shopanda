# Plugin runtime contract (pin-friendly)

Golden summary of Phase 13 capabilities product plugins may rely on. Re-verify
on every Shopanda upgrade.

## Migrations

| Fact | Detail |
| --- | --- |
| Interface | `plugin.Migrator` → `Migration() plugin.MigrationSpec` |
| Apply | `app migrate` / `setup` without `Init`; `InitAll` applies before `Init` |
| Version key | `plugin:{Name()}:{basename}` |
| Runner | `migrate.RunPluginFS` (per-file xact + advisory lock) |

## On-demand jobs

| Fact | Detail |
| --- | --- |
| API | `pluginsdk` `RegisterManual` / `RegisterOnEvent` / `RegisterCron` + `Enqueue` |
| Job type | **`integration.sync.{slug}.{name}` only** |
| Bind | Composition root binds queue after resolve; `Enqueue` before bind fails clearly |
| Free-form types | **Out of scope** (no `myproduct.generate`) |

## HTTP

| Fact | Detail |
| --- | --- |
| Public | `app.RegisterPublicRoute(pattern, handler)` |
| Admin | `RegisterAdminRoute` (see PLUGINS.md) |

## Import boundary

External plugins must not import `internal/infrastructure` or `internal/interfaces`.
Enforced by `go test ./plugins/ -run ImportBoundary`.

## Smoke

```bash
go test ./plugins/ -run ImportBoundary -count=1
go test ./pkg/pluginsdk/ ./plugins/example/ ./plugins/sitekit/ -count=1
```
