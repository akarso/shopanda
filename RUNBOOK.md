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

## Metrics (Prometheus)

- **Disabled by default** (`metrics.enabled: false`). No `/metrics` listener starts at all — enabling nothing costs nothing.
- When enabled, `/metrics` is served on a **dedicated listener** (`metrics.listen`, default `127.0.0.1:9090`) — not on the main app port. It is never merged into the public API surface or its middleware stack (no rate limit, no auth, no CORS).
- **This endpoint has no built-in authentication.** The loopback-only default is the safety net: only change `metrics.listen` to a non-loopback address if the scrape path stays on a private network a scraper (e.g. Prometheus) can reach but the public internet cannot (Docker/Kubernetes internal network, VPN, or a reverse-proxy scrape rule with its own auth). Never bind it to a public interface directly.
- Both `serve`/`dev` and standalone `worker` processes expose `/metrics` when enabled — each process only reports the metrics it can observe (the worker process has no HTTP requests to report; the API process still reports job failures for jobs enqueued from HTTP paths).
- **Colocated serve + worker:** both default to `127.0.0.1:9090`. Only one process can bind that port — set distinct `metrics.listen` values (e.g. `127.0.0.1:9090` for serve, `127.0.0.1:9091` for worker) when both run on the same host with metrics enabled. Bind failures fail **startup** (not a silent async log).
- **Production bind policy:** startup rejects `metrics.listen` on all interfaces (`0.0.0.0`, `::`) and on non-loopback addresses unless `SHOPANDA_DEV_MODE` is truthy. Default loopback + local scraper is the supported production path.
- Env overrides: `SHOPANDA_METRICS_ENABLED`, `SHOPANDA_METRICS_LISTEN`.

**Metrics exposed** (all labels are bounded — fixed enums, route templates, or compile-time job types; never raw URLs, IDs, or emails):

| Metric | Type | Labels | Notes |
| --- | --- | --- | --- |
| `shopanda_http_requests_total` | counter | `route`, `method`, `status_class` | `route` is the matched route **template** (e.g. `GET /api/v1/products/{id}`), not the raw path. `status_class` is `2xx`/`3xx`/`4xx`/`5xx`/`other`, not the numeric code. Unmatched requests (404s) use the fixed label `unmatched`. |
| `shopanda_http_request_duration_seconds` | histogram | `route`, `method` | Same bounded route/method labels. |
| `shopanda_checkout_result_total` | counter | `outcome` (`success`/`failed`) | One increment per `checkout.Workflow.Execute` call (panics count as `failed`). |
| `shopanda_job_failures_total` | counter | `job_type` | Incremented on handler error or "handler not found"; not incremented on success. |
| `shopanda_webhook_deliveries_total` | counter | `outcome` (`success`/`failed`) | Skipped deliveries (inactive/unsubscribed endpoint, malformed job payload) are not counted — they were never attempted. |

**Example Grafana/PromQL queries** (no dashboards ship — build your own from these):

```promql
# Error rate by route (RED: errors)
sum(rate(shopanda_http_requests_total{status_class="5xx"}[5m])) by (route)
  / sum(rate(shopanda_http_requests_total[5m])) by (route)

# p95 latency by route (RED: duration)
histogram_quantile(0.95, sum(rate(shopanda_http_request_duration_seconds_bucket[5m])) by (le, route))

# Checkout failure rate
sum(rate(shopanda_checkout_result_total{outcome="failed"}[5m]))
  / sum(rate(shopanda_checkout_result_total[5m]))

# Job failures by type
sum(rate(shopanda_job_failures_total[15m])) by (job_type)
```

**Known limitation:** no OpenTelemetry tracing yet (tracked as a PR-1020 follow-up); metrics are counters/histograms only, no distributed traces.

## Container releases (GHCR)

Version tags (`v*`) publish `ghcr.io/<owner>/shopanda`. **Pin deploys** to `sha-<full40gitsha>` or `@sha256:…` digest — not the version tag alias. See [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md#pull-from-ghcr-releases).

The `sha-<commit>` tag is never overwritten: the Release workflow refuses to re-push it if it already exists in GHCR (rebuilding the same commit can still yield different image content, since Alpine packages are installed unpinned at build time). A release re-run for an already-released commit fails fast in the "Guard — sha tag must stay immutable" step — this is expected; cut a new commit/tag if you need to publish a fix.

## Supply chain

- Dependabot: weekly PRs for Go modules, Actions, and Docker digests (`.github/dependabot.yml`).
- `CI / govuln`: pinned fail-closed `govulncheck`; exceptions only via [`GOVULN_BASELINE.md`](docs/phase-10-platform-excellence/GOVULN_BASELINE.md). Details in [DEVELOPER.md](docs/guides/DEVELOPER.md#supply-chain-dependabot--govulncheck).

## Outbound webhooks (SSRF)

- Endpoint URLs must be **https**. Loopback, RFC1918, link-local (including cloud metadata `169.254.169.254`), IPv6 ULA, IANA special-purpose IPv4 (CGNAT, TEST-NET, benchmarking `198.18/15`, reserved `240/4`, …), and well-known NAT64 (`64:ff9b::/96`) destinations are rejected at create/update (literal IPs) and again at delivery (DNS resolution). Non-canonical IP hosts (`127.1`, decimal/hex forms) are rejected at create/update.
- Delivery dials only addresses that pass the allow check after resolve; if **any** A/AAAA record is disallowed, delivery fails (DNS-rebinding safe). Redirects remain disabled. `HTTP_PROXY` / `HTTPS_PROXY` are **not** honored for webhook delivery (would bypass destination IP checks).
- Admin API returns validation errors for blocked URLs; failed deliveries surface `ssrf: …` / `webhook post: …` in job errors and worker logs.

## HTTP body limits and security headers

- Default request body cap is **1 MiB** (`http.max_body_bytes` / `SHOPANDA_HTTP_MAX_BODY_BYTES`). Oversized JSON/API bodies return **413** with `error.code=payload_too_large`.
- Admin media uploads (`POST /api/v1/admin/media`, `.../upload`) use **10 MiB** by default (`http.media_max_body_bytes`). The media cap is independent of the JSON default (raising `max_body_bytes` does not raise media).
- Every response sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`. HSTS is added only on TLS or trusted-proxy HTTPS.

## Checkout cancel and timeouts

Checkout uses the HTTP request context. Client disconnects and the server **WriteTimeout** (30s, not configurable — see [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md#http-boundary)) cancel in-flight catalog, pricing, reserve, and PSP **initiate** calls. Logs: `checkout.step.failed` with a context cancel/deadline error.

Compensating work is **detached** (~30s bound) so cancel does not skip it:

| Already committed | Still runs after cancel |
| --- | --- |
| Inventory reserved for earlier cart lines | `Release` those reservations |
| Store credit redeemed, then order save/apply failed | Re-`Issue` the credit |
| PSP already accepted/captured | Persist payment status (avoids a retry minting a new payment ID / double charge) |
| Order saved | Copy cart-item extension snapshots onto the order |

If a shopper closes the tab after the PSP succeeds, you may still see a completed payment and a pending/paid order; do not treat a 499/504 at the proxy as “no charge.” Inventory leftover after a failed checkout should expire at reservation TTL (15 minutes) if a detached release itself failed.

Plugin authors: [PLUGIN_COMPOSITION.md](docs/guides/PLUGIN_COMPOSITION.md) (forward `ctx` vs detached compensate/persist).

## Event bus drain (SIGTERM)

After HTTP connections drain, `serve` stops the embedded scheduler and job worker and **drains async event handlers**, waited **in parallel**. Handler budget is **10 seconds** (`Drain`); `ShutdownBackground` waits **11 seconds** (10s + 1s slack) so `event.bus.drain.timeout` can be logged before the process exits. Event-bus policy is **wait-then-cancel** (not cancel at t=0):

1. Stop starting new `OnAsync` goroutines. In-flight `Publish` still runs **sync** handlers (errors still abort that publish); it does not fail only because the bus is draining.
2. Wait for in-flight `OnAsync` handlers with a **live** context for most of the Drain budget (~9s). Core handlers (order confirmation, webhook enqueue) do a DB lookup + queue insert — they should finish here.
3. Cancel remaining handlers, then wait the last ~1s so they can return. If they still outlive the 10s Drain budget, log `event.bus.drain.timeout` (and `background.shutdown.timeout` if scheduler/worker also overran). The process then exits; remaining handlers are abandoned.

The grace/cancel split is **hardcoded**, not configurable: remainder is `min(DrainTimeout/5, 1s)` (so 10s → 9s grace + 1s after cancel). Plugin authors who need a longer live-context window must keep `OnAsync` bodies short or enqueue a job; there is no env/config override.

`OnAsync` does **not** use the HTTP request context — a shopper abort does not cancel webhooks/notifications already dispatched. Plugin handlers should use the received `ctx` for DB/HTTP/queue calls so a straggler after grace does not block exit.

**Limitation:** there is no outbox/retry. Work that is still in the handler when grace ends is cancelled and not retried (same as a crash). Keep `OnAsync` bodies short (enqueue a job, don’t call the PSP inline). Ignoring `ctx` after cancel can still delay or drop the last second of shutdown.

## Common references

- [EU compliance fields](docs/phase-5-maturity/specs/COMPLIANCE_EU.md) — Omnibus, WEEE, EPR, GPSR
- [Runtime modes](docs/phase-4-refactoring/specs/RUNTIME_MODES.md) — `serve`, `worker`, `scheduler`, `app dev`
- [Commercial B2B module](docs/COMMERCIAL.md) — license-gated `plugins/b2b`
