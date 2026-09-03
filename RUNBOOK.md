# Runbook

The operational guides live in [`docs/guides/`](docs/guides/):

- [**Deployment Guide**](docs/guides/DEPLOYMENT.md) — install, configure, run, scale, back up, and troubleshoot Shopanda
- [**Developer Guide**](docs/guides/DEVELOPER.md) — extend the platform via plugins, events, pipelines, and workflows
- [**Extension Points**](docs/guides/EXTENSION_POINTS.md) — which mechanism (events, hooks, pricing pipeline, checkout workflow, composition steps, registries) to reach for
- [**Merchant Guide**](docs/guides/MERCHANT.md) — manage products, orders, and day-to-day store operations

## Incident response

Symptom → check → fix, for the scenarios most likely to page someone. Each links back to the fuller reference section below it for detail this summary skips.

### Database unreachable

**Symptom:**
- At startup: `serve`/`worker` exits immediately, stderr `error: database: db: ping: <driver error>`. Both processes behave identically — neither retries, neither starts in a degraded mode.
- Mid-run: `GET /readyz` returns `503 {"status":"unavailable"}` while `GET /healthz` stays `200` — the process is alive but can't reach Postgres. Regular API requests on DB-backed routes return `5xx` with no single unifying log line; check `http.request` log entries for `status >= 500` on the affected routes.
- Worker process: repeating `worker.dequeue.failed` log lines, one per poll tick — the worker does **not** crash, it keeps polling indefinitely.

**Check:**
```bash
curl -s -o /dev/null -w '%{http_code}\n' localhost:PORT/healthz   # expect 200 — rules out a crashed process
curl -s -o /dev/null -w '%{http_code}\n' localhost:PORT/readyz    # 503 confirms DB unreachable from this process
pg_isready -h <host> -p <port>                                    # or: psql "$DATABASE_URL" -c 'select 1'
```

**Fix:** restore Postgres connectivity (network/credentials/DB itself). If the DB was down at process **start**, the process already exited and needs a manual/orchestrator restart once connectivity is confirmed. If it dropped **mid-run**, both `/readyz` and the worker self-heal on the next successful ping/dequeue — no restart needed. See ["Liveness vs readiness"](#liveness-vs-readiness) below for the full probe contract.

### Bad migration

**Symptom:** `./shopanda migrate` exits non-zero: `migrate <file>: exec: <pg error>` or `migrate <file>: record version: <pg error>`.

**Check:**
```bash
psql "$DATABASE_URL" -c "select version from schema_migrations order by version desc limit 5;"
```
The failing file is **not** in this table — each migration runs inside one transaction (`BEGIN` … exec … record version … `COMMIT`), so a failure rolls back cleanly and leaves the schema exactly as it was before that file ran.

**Fix:** fix the `.sql` file (or author a new corrective migration with the next numeric prefix — never renumber or edit an already-shipped one), then re-run `./shopanda migrate`. It only (re)attempts files not yet recorded, so retrying is safe. Exception: a statement that cannot run inside a transaction block (e.g. `CREATE INDEX CONCURRENTLY`) fails differently — Postgres may leave a partial object behind; inspect manually (`\d <table>`, `pg_stat_progress_create_index`) rather than assuming the same clean rollback applies.

There is **no down-migration mechanism** in this codebase. "Rollback" for a schema change means restoring from backup, not reversing a migration file — see [DEPLOYMENT.md's backup/restore commands](docs/guides/DEPLOYMENT.md) (`pg_dump` / `psql`).

### Rollback (deploy / migration / plugin config)

What actually exists today, so you don't go looking for tooling that isn't there:

| What broke | Rollback path | What does **not** exist |
| --- | --- | --- |
| Bad container image / deploy | Manually redeploy the previous known-good `sha-<commit>` / digest pin | No scripted rollback command anywhere in this repo — **keep a record of the last-known-good tag before every deploy**, nothing here tracks deploy history for you |
| Bad migration | Restore from backup (`pg_dump`/`psql`, see [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md)) | No down-migrations, no `migrate down` |
| Bad plugin config | Revert the config flag/env var, then restart | No runtime plugin disable/enable API. `serve` degrades and keeps running (`plugin.init.failed` logged, other plugins unaffected); `worker`/`scheduler`/`search:reindex` instead **exit non-zero** on the same failure — either way, a restart is required after fixing the config |

### Webhook SSRF rejects

**Symptom:** admin create/update of a webhook endpoint returns a validation error `ssrf: destination address ... is not allowed` (or `ssrf: non-canonical IP host is not allowed`); or a delivery job repeatedly fails with `worker.job.failed` whose error contains `ssrf: destination resolves to disallowed address ...` — the domain looks fine but its DNS record currently resolves into a blocked range.

**Check:**
```bash
dig +short <endpoint-host>   # what does it resolve to right now?
```
Compare against the blocked ranges in ["Outbound webhooks (SSRF)"](#outbound-webhooks-ssrf) below (private/link-local/reserved/NAT64). Also confirm the endpoint is `https://` — a plain `http://` URL is rejected regardless of IP and can look like an SSRF false positive at a glance.

**Fix:** if the destination is genuinely private/reserved, this is the control working as intended — no fix needed. If it's a legitimate public endpoint that happens to resolve into a blocked range, **there is currently no allowlist or override** — onboarding it requires a code change to `internal/platform/ssrf`, not a config flag. Do not attempt to route around this check; escalate to engineering.

### Rate-limit / login lockouts

**Symptom:** a specific user gets `429` / `too many login attempts, try again later` on login; or general API clients get `429` / `rate_limited`.

**Check:** these are two different limiters — distinguish which one fired. Login lockout is keyed by **IP + normalized email** (`auth.lockout`); a single user locked out on login while their other API calls succeed is lockout, not the general HTTP rate limit. Confirm configured thresholds: `auth.lockout.max_failures` (default 10), `auth.lockout.window` (default 15m), `auth.lockout.store` (`cache` default, shared across instances; `memory` is single-instance only — logs `auth.lockout.store_memory` at startup if selected).

**Fix:** **there is no admin unlock endpoint or CLI command today.** The remediation paths that actually exist:
- Wait it out — lockout self-clears after one full quiet window (default 15 minutes) with no further failed attempts.
- `store=memory` only: restarting the affected instance clears lockout state — but for **every** locked-out key on that instance, not just the one you meant to clear.
- `store=cache`: technically clearable by deleting the backend cache key directly (`auth:lockout:` + SHA-256 of `"<ip>|<lowercased, trimmed email>"` — the email **must** be lowercased and trimmed exactly as the app does before hashing, or the deletion silently targets the wrong key: no error, key just isn't found, and the user stays locked out with no indication anything went wrong). This needs direct cache access and manually computing the hash — no supported tool does this for you, and this is not a safe first move under incident pressure. Prefer waiting it out (default 15 minutes) unless the situation genuinely can't wait.

For general `429`/`rate_limited` (not login-specific): this is per-process and not shared across instances. If a shared NAT/proxy is causing false positives across unrelated clients, set `rate_limit.trusted_proxies` so `ClientIP` reflects the real client — see ["Rate limiting and login lockout"](#rate-limiting-and-login-lockout) below.

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

## Database connection pooling (PgBouncer)

- If PgBouncer (or similar) sits in front of PostgreSQL in **transaction-pooling mode**, set `database.query_exec_mode: exec` (or `SHOPANDA_DATABASE_QUERY_EXEC_MODE=exec`). Otherwise pgx defaults to server-side prepared statement caching, which does not survive that pooling mode — symptom: `prepared statement "..." already exists` / `does not exist` errors under load.
- Leave unset for a direct connection to PostgreSQL — it trades away a real performance optimization and should only be set when a transaction-pooling proxy is actually in front of the database. See [DEPLOYMENT.md](docs/guides/DEPLOYMENT.md) ("Consider connection pooling").

## Admin store credit issuance

- `POST /api/v1/admin/customers/{customerId}/store-credit/issue` mints store credit; gated by the dedicated `customers.store_credit.write` permission (`RoleAdmin` only by default), not `customers.write`.
- **Cap:** a single issuance is capped at `store_credit.max_issue_amount` (minor units, default `100000`; `0` disables the cap). Set this to your actual risk tolerance — the default is a conservative placeholder, not a business-approved limit.
- **Idempotency:** send an `Idempotency-Key` header on issuance requests. A retried request with the same key (e.g. after a client timeout with an ambiguous response) is a no-op instead of crediting twice; requests without the header are unprotected, matching pre-existing behavior. Uniqueness is per-customer, enforced by a DB constraint (migration `064_store_credit_idempotency.sql`), not just an in-process check.
- Every issuance (success or failure) is written to the admin audit log (`AuditStoreCreditIssue`), same as payments/returns.

## Admin jobs (retry/cancel)

- `GET /api/v1/admin/jobs` (list, filter by `type`/`status`, paginated) and `GET /api/v1/admin/jobs/{id}` (detail, incl. `payload` and `last_error`) are gated by `jobs.read`; `POST /api/v1/admin/jobs/{id}/retry` and `POST /api/v1/admin/jobs/{id}/cancel` are gated by `jobs.write`. Both are admin-only (`RoleAdmin`), same as `audit.read` — no other role has them by default.
- **Retry** only works on a job currently `failed`: transitions its status from `failed` back to `pending`, and resets `attempts` to `0` and `run_at` to now so the worker picks it up again on its next poll. Any other status (`pending`, `processing`, `done`, `cancelled`) returns **409 conflict** naming the job's actual status — retry is never a silent no-op.
- **Cancel** only works on a job currently `pending`: flips it to the terminal `cancelled` status so it is never dequeued. **There is no in-flight cancellation** — a `processing` job cannot be cancelled; the 409 response says so explicitly (`"job is currently processing and cannot be cancelled — ... wait for it to complete or fail, then retry if needed"`), since this is the most likely point of operator confusion.
- **Postgres-only.** Both the read endpoints and retry/cancel require the configured job queue driver to satisfy the `jobs.Reader`/`jobs.Admin` ports — true for the built-in Postgres queue, not for a broker-backed driver (Redis, RabbitMQ, Kafka, SQS). If a non-Postgres driver is configured, `serve` fails at startup with a clear error rather than silently omitting these routes.
- Every list/get/retry/cancel call is written to the admin audit log (`job.list`, `job.read`, `job.retry`, `job.cancel`; `ResourceType: "job"`), same as payments/returns/store credit.
- No bulk operations yet (e.g. "retry all failed of type X") — single-job actions only.
- **CLI equivalents** (PR-1032, for deploy scripts/CI that shouldn't need HTTP + auth): `app jobs:list [--type=X] [--status=Y] [--limit=N] [--offset=N] [--json]`, `app jobs:show <id> [--json]`, `app jobs:retry <id>`, `app jobs:cancel <id>` — same `jobsApp.Service` calls as the HTTP routes above, same retry/cancel status rules, non-zero exit on any failure. `jobs:retry`/`jobs:cancel` write to the same admin audit log the HTTP routes do (`job.retry`/`job.cancel`), with `AdminID` set to `cli:<os-user>` (there's no authenticated admin session in a CLI invocation) instead of a real admin user ID — check the audit log's `admin_id` column for that prefix when tracing a CLI-initiated action. `jobs:list` defaults to the same limit/offset the HTTP route does and prints a "there may be more" hint when a page comes back full, instead of silently truncating.

## Admin schedules (list/trigger/enable/disable)

- `GET /api/v1/admin/schedules` is gated by `jobs.read`; `POST /api/v1/admin/schedules/{name}/trigger`, `.../enable`, `.../disable` are gated by `jobs.write` — same permissions as job admin (schedules and jobs are the same operational surface).
- **The scheduler runs as a separate OS process from the API server in production** (`./app scheduler`, not embedded in `./app serve` — see "Runtime Modes" in the Developer Guide). Because of that, this admin surface is Postgres-backed, not read from this server's own memory:
  - **List** always reflects reality — whichever process is actually running (the standalone `scheduler` command in production, or `serve` with an embedded scheduler in dev) upserts its registered task names/specs into `scheduler_tasks` every time it starts.
  - **Enable/disable** always works and takes effect in the real running scheduler without it needing a restart — its tick loop checks the same `scheduler_tasks.enabled` column on every tick before firing a task.
  - **Trigger is the one operation that needs a live scheduler in *this* process.** In a standard production deployment (`serve` without an embedded scheduler), triggering returns **409 conflict** — `"this server process has no embedded scheduler to trigger from"` — because there is no in-process function to call. It works immediately in `dev` mode (`./app dev`, which embeds the scheduler by default) or any `serve` deployment that explicitly opts into embedding.
  - If you *do* see `409` with a different message — `"is registered but not part of this process's local scheduler"` — that means more than one process is embedding a scheduler with different task sets (e.g. two `serve --embed-scheduler` instances with different plugin configs). Running more than one embedded scheduler is unsupported: each one independently fires its own tick, so more than one active at a time double-fires every scheduled task. Fix by running exactly one embedded scheduler, or none (standalone `./app scheduler`, the recommended production setup).
- A disabled task can still be triggered manually — disabling only stops the *automatic* tick from firing it, exactly like a paused-but-manually-runnable job.
- Unknown task name: **404**, on any of the four endpoints.
- Every list/trigger/enable/disable call is written to the admin audit log (`schedule.list`, `schedule.trigger`, `schedule.enable`, `schedule.disable`; `ResourceType: "schedule"`).
- **CLI equivalents** (PR-1032): `app schedule:list [--json]`, `app schedule:enable <name>`, `app schedule:disable <name>` call the exact same `schedulerApp.Service` methods as the HTTP routes above (Postgres-backed, always work regardless of process), and write to the same admin audit log the HTTP routes do (`schedule.enable`/`schedule.disable`/`schedule.trigger`), with `AdminID` set to `cli:<os-user>` instead of a real admin user ID (there's no authenticated admin session in a CLI invocation).
- **`app schedule:trigger <name>` does not call the same method the HTTP route does** — the CLI process has no embedded scheduler to reuse either, so instead of hitting the same 409 a `serve`-without-embedding process would, it registers every task itself (mirroring `./app scheduler`'s own registration, including plugin-registered cron jobs) and fires the requested one directly, then exits once it's done. This means CLI trigger always works where HTTP trigger against production `serve` doesn't — that's by design (see the source comment on `runScheduleTrigger`), not a bug in either.
- **`schedule:trigger` pays the full plugin-bootstrap cost on every call** (`registry.InitAll`, permission registry freeze, stock syncer wiring) — the same construction `./app scheduler` does at its own startup, needed so a plugin-registered cron task can be triggered too, not just the four built-in ones. If any installed plugin's `Init()` has side effects (webhook (re-)registration, background goroutines, outbound calls), those fire on **every** `schedule:trigger` invocation, even when triggering a task unrelated to that plugin. Fine for occasional manual/ops use; think twice before wiring `schedule:trigger` into a tight automated loop (e.g. a CI step that runs on every commit) if any installed plugin does non-idempotent work in `Init()`.

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
- **Colocated serve + worker:** both would otherwise default to `127.0.0.1:9090`. The worker process automatically shifts to `127.0.0.1:9091` when it detects `metrics.listen` is still the unmodified default, so the common case needs no manual configuration. Set `metrics.listen` explicitly on both processes (via `SHOPANDA_METRICS_LISTEN`) if you want different addresses/ports than that. Bind failures fail **startup** (not a silent async log).
- The metrics `http.Server` sets the same `ReadTimeout`/`WriteTimeout`/`IdleTimeout` as the main server (10s/30s/60s) plus a 5s `ReadHeaderTimeout`, since this listener has no auth of its own.
- **Production bind policy:** startup rejects `metrics.listen` on all interfaces (`0.0.0.0`, `::`) and on non-loopback addresses unless `SHOPANDA_DEV_MODE` **or** `SHOPANDA_METRICS_ALLOW_INSECURE_BIND` is truthy. The two are independent on purpose — enabling external Prometheus scraping should not also weaken the DB password/SSL checks that `SHOPANDA_DEV_MODE` gates. Default loopback + local scraper is the supported production path.
- The `method` label on `shopanda_http_requests_total`/`_duration_seconds` is bounded to `GET/HEAD/POST/PUT/PATCH/DELETE/OPTIONS`; anything else (malformed or attacker-supplied) is recorded as `other`.
- Env overrides: `SHOPANDA_METRICS_ENABLED`, `SHOPANDA_METRICS_LISTEN`, `SHOPANDA_METRICS_ALLOW_INSECURE_BIND`.

**Metrics exposed** (all labels are bounded — fixed enums, route templates, or compile-time job types; never raw URLs, IDs, or emails):

| Metric | Type | Labels | Notes |
| --- | --- | --- | --- |
| `shopanda_http_requests_total` | counter | `route`, `method`, `status_class` | `route` is the matched route **template** (e.g. `GET /api/v1/products/{id}`), not the raw path. `status_class` is `2xx`/`3xx`/`4xx`/`5xx`/`other`, not the numeric code. Unmatched requests (404s) use the fixed label `unmatched`. |
| `shopanda_http_request_duration_seconds` | histogram | `route`, `method` | Same bounded route/method labels. |
| `shopanda_checkout_result_total` | counter | `outcome` (`success`/`failed`/`succeeded_event_failed`) | One increment per `checkout.Workflow.Execute` call (panics count as `failed`). `succeeded_event_failed` is every step succeeding but the final `checkout.completed` event publish failing — a downstream/event-bus problem on an otherwise-successful order, not a checkout failure. |
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

**Historical note:** the OpenTelemetry tracing gap noted here through PR-1023 is closed by PR-1024 — see the next section.

## Tracing (OpenTelemetry)

- **Disabled by default** (`tracing.enabled: false`). No exporter, no background export goroutine, no cost — the same "enabling nothing costs nothing" posture as metrics.
- When enabled, spans export via **OTLP/HTTP** to `tracing.endpoint` (host:port, no scheme — e.g. `localhost:4318`, not `http://localhost:4318`; a scheme prefix is rejected at startup, not left to fail at first export).
- **Two spans per checkout, one per HTTP request:** every HTTP request gets a span named `HTTP {method}` with `http.route` (the matched template, e.g. `/products/{id}` — never the raw URL) and `http.response.status_code` attributes. Every `checkout.Workflow.Execute` call gets a root span `checkout.execute` plus one child span per step (`checkout.step.<name>`), each with its own error status on failure.
- **No raw URLs or path segments in span attributes** — same reasoning as the metrics label policy above, applied to what leaves the process via OTLP instead of via `/metrics`. A customer ID, order ID, or reset token embedded in a path never gets exported.
- **`tracing.insecure: true` (cleartext OTLP export) is validated, not just discouraged in a comment.** Startup rejects it unless `SHOPANDA_DEV_MODE` is truthy or the endpoint resolves to a loopback host — same posture as `database.sslmode=disable`. Use TLS (the default) against anything that isn't a same-host/local collector.
- **Sampling:** `tracing.sample_ratio` (0.0–1.0) defaults to `1.0` (sample everything) when left unset. Setting it to `0` is a deliberate "instrument the code, export nothing" state, not the same as leaving it unset — both survive distinctly through config loading. Values above `1` are clamped down to `1` (typo defense — `150` is far more likely to mean "150%, i.e. everything" than a real attempt to oversample); negative values are rejected at startup.
- **Headers** (`tracing.headers`, a map) are sent with every OTLP export request — use this for a collector API key (Grafana Cloud, Honeycomb, etc. commonly require one). Redacted (`***`) wherever config values are surfaced through `Get()`/`GetOrDefault()`.
- Both `serve` and standalone `worker` processes call the same `tracing.Setup`, tagged with a distinct `service.name` resource attribute (`shopanda-api` / `shopanda-worker`) so a collector can tell them apart. The worker process doesn't currently instrument any job handler — `Setup` is wired for parity and future use, not because job spans exist yet.
- Env overrides: `SHOPANDA_TRACING_ENABLED`, `SHOPANDA_TRACING_ENDPOINT`, `SHOPANDA_TRACING_INSECURE`, `SHOPANDA_TRACING_SAMPLE_RATIO`. No env mapping for `headers` (a map) — set it in YAML.
- **Local collector (Jaeger):**
  ```bash
  docker run -d --name jaeger -p 16686:16686 -p 4318:4318 jaegertracing/all-in-one:latest
  ```
  Then set `tracing.enabled: true`, `tracing.endpoint: "localhost:4318"`, `tracing.insecure: true` (loopback, so this is allowed without `SHOPANDA_DEV_MODE`) — traces appear at `http://localhost:16686`.
- **Grafana Cloud (or any TLS OTLP/HTTP collector requiring an API key):** `tracing.endpoint: "otlp-gateway-<region>.grafana.net:443"`, `tracing.insecure: false` (the default), `tracing.headers: {Authorization: "Basic <base64 instance-id:api-key>"}`. Consult your collector's OTLP/HTTP ingest docs for the exact header format — this varies by vendor.
- **Known limitation:** DB query spans are not implemented (see PR-1024's "Scope decisions" — no shared `*sql.DB` access wrapper exists yet to hang them on) and no job/worker-level span exists yet.

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

If a shopper closes the tab after the PSP succeeds, you may still see a completed payment and a pending/paid order; do not treat a 499/504 at the proxy as “no charge.” Inventory leftover after a failed checkout stops counting against availability immediately (stock queries filter `expires_at > now()` at read time) even if a detached release itself failed — but the reservation row itself only actually flips to `released` status via a scheduled job (`inventory.reservation_expiry`, every 15 minutes, matching the reservation TTL) that sweeps everything expired as of the sweep time. Between expiry and the next sweep, the row still reads `active` even though it no longer reserves anything — don't treat `active` status alone as proof a reservation is still live; check `expires_at` too. On a deployment upgrading from before this job existed, the sweep also picks up the entire historical backlog of already-expired rows (the query has no "only new" filter), not just newly-expired ones. It processes in bounded batches (500 rows/transaction, `FOR UPDATE SKIP LOCKED`) so a large backlog never holds locks on `stock`/`reservations` rows for more than one batch's duration — concurrent checkout `Reserve` calls on the same variants are not blocked for the sweep's full length. Each invocation is itself capped at 10 minutes; if a backlog is large enough that one tick doesn't finish, it logs `more_remaining: true` and picks up where it left off on the next scheduled tick 15 minutes later — expect several ticks, not one long one, to fully drain a very large backlog.

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
