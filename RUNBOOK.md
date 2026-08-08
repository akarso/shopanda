# Runbook

The operational guides live in [`docs/guides/`](docs/guides/):

- [**Deployment Guide**](docs/guides/DEPLOYMENT.md) — install, configure, run, scale, back up, and troubleshoot Shopanda
- [**Developer Guide**](docs/guides/DEVELOPER.md) — extend the platform via plugins, events, pipelines, and workflows
- [**Merchant Guide**](docs/guides/MERCHANT.md) — manage products, orders, and day-to-day store operations

## Planning

| Phase | Status | Doc |
| --- | --- | --- |
| Phase 5 — Mature commerce | **Complete** | [Roadmap](docs/phase-5-maturity/ROADMAP.md) |
| Phase 6 — Merchant-complete admin | Active | [Roadmap](docs/phase-6-merchant-complete/ROADMAP.md) |

## Plugin extension (operators & integrators)

Shopanda plugins are **compile-time registered** — there is no `.so` drop-in loader.

- Enable **core plugins** via config driver switches (`search.engine`, `queue.driver`, …).
- Enable **external plugins** via config flags after they are registered in `cmd/api/register_plugins.go`.
- Failed plugin init is logged as `plugin.init.failed`; the process continues with other plugins.

**Why no `.so` loading?** See [Dynamic plugin loading research](docs/phase-5-maturity/specs/DYNAMIC_PLUGIN_LOADING.md) (PR-544). Adding a custom external plugin requires rebuilding the binary with your plugin import.

**Check plugin status:** startup logs emit `plugin.status` per registered plugin (`active` / `failed`). Use `app help` to list plugin CLI commands when enabled.

## Rate limiting and login lockout

- HTTP rate limiting is **on by default** (`SHOPANDA_RATE_LIMIT_ENABLED=true`). Each application process enforces its own default of **10 requests/second** with a **burst of 20**. Limits are **not** shared across instances — use a gateway or WAF for a global HTTP ceiling. Rejected requests return `429` / `rate_limited`.
- Failed password logins are throttled by **IP + normalized email** (`auth.lockout`). After `max_failures` within the lockout window, login returns `429` (`too many login attempts, try again later`). Wrong-password responses remain uniform `unauthorized` before the threshold.
- **Sliding window:** each failed attempt refreshes the full `auth.lockout.window` TTL. Continued failures can extend lockout; quiet time of one full window after the last failure is when lockout ends (or a successful login clears the counter).
- **Multi-instance lockout:** `auth.lockout.store=cache` (default) shares counters via cache (postgres/redis) but uses non-atomic Get/Set — concurrent increments can under-count (best-effort, not a strict lockout). `store=memory` is single-instance only; startup logs a warning if selected.
- Behind a reverse proxy, set `rate_limit.trusted_proxies` in YAML (CIDR / IP list; see `configs/config.example.yaml`) so ClientIP for rate limit + lockout is not the proxy address. No env mapping for trusted proxies.

## Common references

- [EU compliance fields](docs/phase-5-maturity/specs/COMPLIANCE_EU.md) — Omnibus, WEEE, EPR, GPSR
- [Runtime modes](docs/phase-4-refactoring/specs/RUNTIME_MODES.md) — `serve`, `worker`, `scheduler`, `app dev`
- [Commercial B2B module](docs/COMMERCIAL.md) — license-gated `plugins/b2b`
