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
- **Reserve-before-verify:** each login atomically increments the counter before password comparison, so a concurrent batch for the same IP+email cannot evaluate more than `max_failures` guesses. A successful login clears the reservation (and prior failures) via compare-and-subtract.
- **Sliding window:** each reserved/failed attempt refreshes the full `auth.lockout.window` TTL. Continued failures can extend lockout; quiet time of one full window after the last failure is when lockout ends (or a successful login clears the counter).
- **Multi-instance lockout:** `auth.lockout.store=cache` (default) shares counters via cache (postgres/redis) using atomic `Incr` (sliding window TTL). `store=memory` is single-instance only; startup logs a warning if selected.
- Behind a reverse proxy, set `rate_limit.trusted_proxies` in YAML (CIDR / IP list; see `configs/config.example.yaml`) so ClientIP for rate limit + lockout is not the proxy address. No env mapping for trusted proxies. The same list gates HSTS (`Strict-Transport-Security`) via `X-Forwarded-Proto: https`.

## Secure-by-default config (startup)

- Startup **rejects** DB passwords `changeme` / `shopanda` unless `SHOPANDA_DEV_MODE` is truthy (`1`/`true`/`yes`).
- `sslmode=disable` / `prefer` / `allow` (or missing `sslmode` on `DATABASE_URL`) are allowed only with truthy `SHOPANDA_DEV_MODE` **and** a local DB host (`localhost`, `127.0.0.1`, `::1`, or compose service `postgres`). Otherwise use `require` / `verify-ca` / `verify-full`. When `DATABASE_URL` is set, only that DSN is checked (YAML/`SHOPANDA_DATABASE_*` are not merged).
- Plaintext password-reset token logging requires **both** `SHOPANDA_DEV_MODE` and `SHOPANDA_DEV_LOG_RESET_TOKENS` truthy. See [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md).
- Local `docker compose` defaults `SHOPANDA_DEV_MODE=true` and `SHOPANDA_DATABASE_SSLMODE=disable` (stock Postgres image has no TLS) via `${…:-…}` — **omit/empty still applies those defaults**. For production-like compose: set `SHOPANDA_DEV_MODE=false` (or `0`/`no`), override `SHOPANDA_DATABASE_PASSWORD` with a strong non-default value (**do not keep compose `changeme`**), and set `SHOPANDA_DATABASE_SSLMODE=require|verify-ca|verify-full`. `.env.example` leaves sslmode unset so the compose default applies after `cp`.

## Liveness vs readiness

| Probe | Endpoint | Use |
| --- | --- | --- |
| Liveness | `GET`/`HEAD` `/healthz` | Process up (static 200, `Cache-Control: no-store`). Docker image `HEALTHCHECK` stays here. Served **outside** store/auth middleware. |
| Readiness | `GET`/`HEAD` `/readyz` | DB `PingContext` within ~2s → 200; else **503**. Dedicated per-IP probe rate limit (defaults match HTTP rate limit). Point load balancers / k8s readiness here. Prefer network policy so probes are not internet-public. |

If `/readyz` returns 503 while `/healthz` is 200, the API process is up but cannot reach Postgres (or the ping timed out).

## Container releases (GHCR)

Version tags (`v*`) publish `ghcr.io/<owner>/shopanda`. **Pin deploys** to `sha-<full40gitsha>` or `@sha256:…` digest — not the version tag alias. See [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md#pull-from-ghcr-releases).

The `sha-<commit>` tag is never overwritten: the Release workflow refuses to re-push it if it already exists in GHCR (rebuilding the same commit can still yield different image content, since Alpine packages are installed unpinned at build time). A release re-run for an already-released commit fails fast in the "Guard — sha tag must stay immutable" step — this is expected; cut a new commit/tag if you need to publish a fix.

## Outbound webhooks (SSRF)

- Endpoint URLs must be **https**. Loopback, RFC1918, link-local (including cloud metadata `169.254.169.254`), IPv6 ULA, IANA special-purpose IPv4 (CGNAT, TEST-NET, benchmarking `198.18/15`, reserved `240/4`, …), and well-known NAT64 (`64:ff9b::/96`) destinations are rejected at create/update (literal IPs) and again at delivery (DNS resolution). Non-canonical IP hosts (`127.1`, decimal/hex forms) are rejected at create/update.
- Delivery dials only addresses that pass the allow check after resolve; if **any** A/AAAA record is disallowed, delivery fails (DNS-rebinding safe). Redirects remain disabled. `HTTP_PROXY` / `HTTPS_PROXY` are **not** honored for webhook delivery (would bypass destination IP checks).
- Admin API returns validation errors for blocked URLs; failed deliveries surface `ssrf: …` / `webhook post: …` in job errors and worker logs.

## HTTP body limits and security headers

- Default request body cap is **1 MiB** (`http.max_body_bytes` / `SHOPANDA_HTTP_MAX_BODY_BYTES`). Oversized JSON/API bodies return **413** with `error.code=payload_too_large`.
- Admin media uploads (`POST /api/v1/admin/media`, `.../upload`) use **10 MiB** by default (`http.media_max_body_bytes`). The media cap is independent of the JSON default (raising `max_body_bytes` does not raise media).
- Every response sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`. HSTS is added only on TLS or trusted-proxy HTTPS.

## Common references

- [EU compliance fields](docs/phase-5-maturity/specs/COMPLIANCE_EU.md) — Omnibus, WEEE, EPR, GPSR
- [Runtime modes](docs/phase-4-refactoring/specs/RUNTIME_MODES.md) — `serve`, `worker`, `scheduler`, `app dev`
- [Commercial B2B module](docs/COMMERCIAL.md) — license-gated `plugins/b2b`
