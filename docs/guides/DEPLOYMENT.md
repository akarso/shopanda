# Deployment Guide

This guide is for operators who deploy, host, and maintain Shopanda.

For day-to-day store use after the application is running, see [Merchant Guide](MERCHANT.md).

Start with the simplest path first: one host, one PostgreSQL database, and the Shopanda binary. Docker is covered later as an optional packaging and deployment approach.

## Run As A Service

If you plan to run the Go binary under a service manager, see [Deploy On Bare Metal](#deploy-on-bare-metal) and [Example systemd units](#example-systemd-units). That section covers running `shopanda serve`, `shopanda worker`, and `shopanda scheduler` as long-lived host services.

## Quick Start Without Docker

Use this path when you want the simplest working deployment with the least tooling overhead.

### 1. Clone the repository

```bash
git clone https://github.com/akarso/shopanda.git
cd shopanda
```

### 2. Create configuration

If you want an interactive setup flow that writes `.env` for you, run:

```bash
./install.sh
```

If you prefer manual configuration, copy the example file instead:

```bash
cp .env.example .env
```

At minimum, set:

- `SHOPANDA_AUTH_JWT_SECRET`
- `SHOPANDA_SERVER_PUBLIC_BASE_URL`
- database settings via either `DATABASE_URL` or the `SHOPANDA_DATABASE_*` variables

Generate a JWT secret with:

```bash
openssl rand -hex 32
```

If you want the seeded admin account on first setup, add this before running `setup`:

```bash
SHOPANDA_SEED_ADMIN_PASSWORD=change-me-now
```

If you prefer YAML over environment-only configuration, use `configs/config.yaml` or start from `configs/config.example.yaml`, but keep secrets in environment variables or another protected secret store rather than in YAML.

### 3. Build the binary

```bash
go build -o shopanda ./cmd/api
```

If you already have a compiled binary, skip this step.

Do **not** commit build outputs (`shopanda`, `api`, or other binaries) to git. Ship releases via a local/CI build or a container image (see [Deploy With Docker](#deploy-with-docker)). The root `api` binary is gitignored.

### 4. Run first-time setup

**Option A — CLI (operators):**

```bash
./shopanda setup
```

**Option B — Web wizard (merchants):**

Start the server first (step 5), then open `/setup` in a browser. The wizard runs migrations, seeds defaults, and creates the first admin account. Visiting `/admin` before setup completes redirects to `/setup` ([PR-903](../phase-9-merchant-discovery/prs/PR-903.md)).

The setup command / wizard:

- checks database connectivity
- runs migrations
- runs default seeders unless `--skip-seed` is used (CLI only)
- prints store, admin API, and docs URLs (CLI) or links to `/admin` (web)

If you prefer explicit CLI commands instead of `setup`:

```bash
./shopanda migrate
./shopanda seed
```

### 5. Start the application

Start the HTTP server:

```bash
./shopanda serve
```

Run a worker if you depend on async jobs such as email delivery:

```bash
./shopanda worker
```

Run the scheduler if you depend on recurring jobs:

```bash
./shopanda scheduler
```

### 6. Verify the deployment

```bash
curl http://localhost:8080/healthz
curl -f http://localhost:8080/readyz
open http://localhost:8080/docs
open http://localhost:8080/admin
```

Live endpoints in the current application:

- liveness: `/healthz` (process up; Docker `HEALTHCHECK`)
- readiness: `/readyz` (bounded DB ping; use for traffic / orchestrator readiness)
- API docs UI: `/docs`
- OpenAPI spec: `/docs/openapi.yaml`
- admin SPA: `/admin`

### 7. Docker is optional

If you prefer containers after the simple host-based path, skip ahead to [Deploy With Docker](#deploy-with-docker).

## Environment Variables

Shopanda supports both environment variables and YAML config. For production, use either:

- environment variables from your process manager, container platform, or shell
- a checked-in or mounted `configs/config.yaml`

Environment variables override YAML values.

### Server

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_SERVER_HOST` | No | `0.0.0.0` | Bind address |
| `SHOPANDA_SERVER_PORT` | No | `8080` | HTTP port |
| `SHOPANDA_SERVER_PUBLIC_BASE_URL` | Yes for real deployments | none | Public base URL used in generated links and external-facing flows |

### HTTP boundary

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_HTTP_MAX_BODY_BYTES` | No | `1048576` (1 MiB) | Default max request body for non-media routes |
| `SHOPANDA_HTTP_MEDIA_MAX_BODY_BYTES` | No | `10485760` (10 MiB) | Max body for admin media upload routes |

HTTP server timeouts are **fixed** (not env-configurable): **Read 10s**, **Write 30s**, **Idle 60s**. Checkout (and other handlers) use the request context, so WriteTimeout and client disconnect cancel in-flight checkout work. Keep reverse-proxy idle/read timeouts **at least 30s** (preferably a few seconds above WriteTimeout) so the proxy does not cut the connection first and hide the API error. Compensating checkout writes after cancel are documented in [RUNBOOK.md](../../RUNBOOK.md#checkout-cancel-and-timeouts).

Responses always include `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, and `Referrer-Policy: strict-origin-when-cross-origin`. `Strict-Transport-Security` is sent only when the request is TLS (`r.TLS != nil`) or `X-Forwarded-Proto: https` is honored from a peer in `rate_limit.trusted_proxies` — it is **absent** on plain HTTP without a trusted proxy. Terminate TLS at a reverse proxy and list that proxy under `trusted_proxies` so HSTS applies correctly.

### Database

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `DATABASE_URL` | Optional alternative | none | Full PostgreSQL DSN; overrides individual DB fields. When set, secure-by-default checks use **only** this DSN (YAML/`SHOPANDA_DATABASE_*` are not merged). Production DSNs must include an enforcing `sslmode` (`require` / `verify-ca` / `verify-full`). Missing `sslmode` is treated as insecure (`disable`) and is allowed only for local+`SHOPANDA_DEV_MODE` (same rule as structured config). |
| `SHOPANDA_DATABASE_HOST` | Yes unless `DATABASE_URL` is set | `localhost` | PostgreSQL host (validated only when `DATABASE_URL` is unset) |
| `SHOPANDA_DATABASE_PORT` | No | `5432` | PostgreSQL port |
| `SHOPANDA_DATABASE_USER` | Yes unless `DATABASE_URL` is set | `shopanda` | PostgreSQL user |
| `SHOPANDA_DATABASE_PASSWORD` | Yes unless `DATABASE_URL` is set | empty | PostgreSQL password (validated only when `DATABASE_URL` is unset) |
| `SHOPANDA_DATABASE_NAME` | Yes unless `DATABASE_URL` is set | `shopanda` | PostgreSQL database name |
| `SHOPANDA_DATABASE_SSLMODE` | No | `disable` (built-in) / compose `disable` when unset | Used only when `DATABASE_URL` is unset. Production requires `require`, `verify-ca`, or `verify-full`. `disable` / `prefer` / `allow` are allowed only when `SHOPANDA_DEV_MODE` is truthy **and** the DB host is local (`localhost`, `127.0.0.1`, `::1`, or compose service `postgres`). **Docker Compose** defaults to `disable` via `${SHOPANDA_DATABASE_SSLMODE:-disable}` because stock `postgres:17-alpine` has no TLS. |

### Logging

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_LOG_LEVEL` | No | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `SHOPANDA_LOG_FORMAT` | No | `json` | Log format: `json` or `text` |

### Authentication

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_AUTH_JWT_SECRET` | Yes | none | JWT HMAC secret: ≥32 bytes after trim (installer `openssl rand -hex 32` / 64 hex chars accepted as-is). Required; weak/empty values refuse startup |
| `SHOPANDA_AUTH_JWT_TTL` | No | `24h` | Token lifetime |

### Mail

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_MAIL_DRIVER` | No | `smtp` | Mail backend |
| `SHOPANDA_MAIL_SMTP_HOST` | No | `localhost` | SMTP host |
| `SHOPANDA_MAIL_SMTP_PORT` | No | `587` | SMTP port |
| `SHOPANDA_MAIL_SMTP_USER` | No | empty | SMTP username |
| `SHOPANDA_MAIL_SMTP_PASSWORD` | No | empty | SMTP password |
| `SHOPANDA_MAIL_SMTP_FROM` | No | `noreply@localhost` | Sender address |

### Media Storage

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_MEDIA_STORAGE` | No | `local` | Media storage driver |
| `SHOPANDA_MEDIA_LOCAL_BASE_PATH` | No | `./public/media` | Local upload path |
| `SHOPANDA_MEDIA_LOCAL_BASE_URL` | No | `/media` | Public media base URL |

### Cache

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_CACHE_DRIVER` | No | `postgres` | Cache backend |

### Frontend

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_FRONTEND_ENABLED` | No | `false` | Enable the built-in SSR storefront |
| `SHOPANDA_FRONTEND_MODE` | No | `ssr` | Frontend rendering mode |
| `SHOPANDA_FRONTEND_THEME_PATH` | No | `themes/default` | Active storefront theme path |

### CDN

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_CDN_BASE_URL` | No | empty | Optional CDN base URL for asset delivery |

### Webhooks

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_WEBHOOKS_SECRET_STRIPE` | No | empty | Stripe webhook secret |
| `SHOPANDA_WEBHOOKS_SECRET_PAYPAL` | No | empty | Example provider-specific webhook secret |

### Rate Limiting

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_RATE_LIMIT_ENABLED` | No | `true` | Enable request rate limiting |
| `SHOPANDA_RATE_LIMIT_DEFAULT_RATE` | No | `10` | Default tokens per second |
| `SHOPANDA_RATE_LIMIT_DEFAULT_BURST` | No | `20` | Default burst size |
| `SHOPANDA_AUTH_LOCKOUT_ENABLED` | No | `true` | Enable failed-login lockout (IP + account) |
| `SHOPANDA_AUTH_LOCKOUT_STORE` | No | `cache` | `cache` (shared via cache driver) or `memory` (single-instance only) |
| `SHOPANDA_AUTH_LOCKOUT_MAX_FAILURES` | No | `10` | Failures before temporary lockout |
| `SHOPANDA_AUTH_LOCKOUT_WINDOW` | No | `15m` | Lockout counter TTL (Go duration) |

Set `rate_limit.trusted_proxies` in YAML when behind a reverse proxy so both rate limiting and login lockout see the real client IP. Configure it as a list of CIDR (or bare IP) entries — see [`configs/config.example.yaml`](../../configs/config.example.yaml). There is no `SHOPANDA_RATE_LIMIT_TRUSTED_PROXIES` env mapping.

**Multi-instance:** keep `SHOPANDA_AUTH_LOCKOUT_STORE=cache` (default) so counters share the configured `cache.driver` (postgres or redis) via atomic increment. `store=memory` must only be used for single-instance deployments.

### Metrics (Prometheus)

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_METRICS_ENABLED` | No | `false` | Expose Prometheus metrics on a dedicated listener |
| `SHOPANDA_METRICS_LISTEN` | No | `127.0.0.1:9090` | Bind address for the metrics listener (only used when enabled). When **serve and worker** both run on one host with metrics enabled and this is left at the default, the worker process automatically shifts to `127.0.0.1:9091` to avoid colliding with serve. Set both explicitly for a different layout — bind failures fail startup. |
| `SHOPANDA_METRICS_ALLOW_INSECURE_BIND` | No | `false` | Allows a non-loopback/wildcard `metrics.listen` independently of `SHOPANDA_DEV_MODE` — use this instead of dev mode when you only need to relax the metrics bind, since dev mode also disables DB password/SSL checks. |

Metrics are served on a **separate listener** from the main app port, and are **never** wrapped by the main router's rate limit, auth, or CORS middleware — this endpoint has no auth of its own. The loopback-only default means enabling metrics does not expose anything publicly by itself. Startup **rejects** wildcard binds (`0.0.0.0`, `::`) and non-loopback addresses unless `SHOPANDA_DEV_MODE` or `SHOPANDA_METRICS_ALLOW_INSECURE_BIND` is truthy. Only change `metrics.listen` to a non-loopback address in dev, or use loopback + a local/on-host scraper in production. See [RUNBOOK.md](../../RUNBOOK.md#metrics-prometheus) for exposed metric names, label policy, colocated serve/worker ports, and example PromQL queries.

### Seeding

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_SEED_ADMIN_PASSWORD` | Optional | none | When set and `admin@example.com` does not yet exist, `setup` or `seed` creates the seeded admin user with this password. When unset, admin creation is skipped. |

### Development and Testing

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_DEV_MODE` | No | empty | Explicit truthy (`1` / `true` / `yes`) enables local-only relaxations: forbidden DB passwords `changeme`/`shopanda`, and non-enforcing sslmodes on local hosts. Absent/falsey → production-strict startup checks. **Docker Compose** defaults this to `true` via `${SHOPANDA_DEV_MODE:-true}` — omit/empty still enables it; set `false` / `0` / `no` explicitly to override |
| `SHOPANDA_DEV_LOG_RESET_TOKENS` | No | empty | When truthy **and** `SHOPANDA_DEV_MODE` is truthy, logs plaintext password-reset tokens. Never enable in production |
| `SHOPANDA_TEST_DSN` | No | empty | PostgreSQL DSN used by integration tests |

### Production checklist (secure-by-default)

When `DATABASE_URL` is set, apply the checklist to **that DSN only** (password/`sslmode`/`host` inside the URL). When unset, apply it to `SHOPANDA_DATABASE_*` / YAML fields.

- [ ] Strong DB password (not `changeme` / `shopanda`) — in `DATABASE_URL` or `SHOPANDA_DATABASE_PASSWORD`
- [ ] Enforcing TLS: `sslmode=require`, `verify-ca`, or `verify-full` (not `disable` / `prefer` / `allow` / missing)
- [ ] `SHOPANDA_DEV_MODE` unset or falsey (bare metal). On **docker compose**, set `SHOPANDA_DEV_MODE=false` (or `0`/`no`) explicitly — omitting it leaves the compose default `true`
- [ ] On production-like compose: also override `SHOPANDA_DATABASE_PASSWORD` (do not keep compose `changeme`)
- [ ] `SHOPANDA_DEV_LOG_RESET_TOKENS` unset
- [ ] Strong `SHOPANDA_AUTH_JWT_SECRET` (≥32 bytes; see auth section)

### External reference plugins (opt-in)

Reference plugins under `plugins/*demo` are disabled by default. Enable via YAML (`plugins.<name>.enabled`) or environment override. Each plugin must also be registered in `cmd/api/register_plugins.go` (compile-time).

| Variable | Default | Purpose |
| --- | --- | --- |
| `SHOPANDA_PLUGINS_PROMODEMO_ENABLED` | `false` | Enable `plugins/promodemo` — registers custom catalog promotion rule types (`min_line_total`, `line_bonus_percent`) |

See `configs/config.example.yaml` for YAML equivalents and other demo plugin flags (`SHOPANDA_PLUGINS_CARTDEMO_ENABLED`, `SHOPANDA_PLUGINS_MAILDEMO_ENABLED`, …).

### Security notes

- never reuse the sample database password (`changeme` / `shopanda`) in real environments — startup rejects them unless `SHOPANDA_DEV_MODE` is truthy
- treat `SHOPANDA_AUTH_JWT_SECRET` like a production credential; rotate it if leaked
- generate with `openssl rand -hex 32` (64 hex chars). The runtime keeps that string as HMAC/MFA key material (same as prior releases); strength checks require ≥32 bytes after trim
- prefer shell- or platform-injected secrets over committing secrets into YAML
- if you expose Meilisearch to the internet, set a real master key instead of the local-dev default
- never set `SHOPANDA_DEV_LOG_RESET_TOKENS` outside local debugging

## Deploy With Docker

### Build the image

```bash
docker build -t shopanda .
```

Prefer deploying a **CI-built GHCR image** (or a local `docker build`) over checking a compiled binary into the repository.

### Pull from GHCR (releases)

Pushing a git tag matching `v*` (for example `v1.2.3`) runs [`.github/workflows/release.yml`](../../.github/workflows/release.yml), which builds the Dockerfile and pushes to GitHub Container Registry.

Image name: `ghcr.io/<owner>/shopanda` (owner lowercased).

| Tag / reference | Role |
| --- | --- |
| `sha-<full40gitsha>` | **Immutable deploy pin** — the commit that built the image |
| `@sha256:<digest>` | **Immutable deploy pin** — content-addressed digest from the push |
| `v1.2.3` (version tag) | **Mutable alias only** — convenience for humans; may be retargeted; **do not** use as the production pin |

Examples (replace owner / digests):

```bash
# Preferred: pin by full commit SHA tag
docker pull ghcr.io/akarso/shopanda:sha-0123456789abcdef0123456789abcdef01234567

# Preferred: pin by digest (from the Release workflow summary / registry UI)
docker pull ghcr.io/akarso/shopanda@sha256:…

# Optional alias (not for deploy pins)
docker pull ghcr.io/akarso/shopanda:v1.2.3
```

Run a pinned image:

```bash
docker run --rm \
  -p 8080:8080 \
  --env-file .env \
  ghcr.io/akarso/shopanda:sha-<full40gitsha>
```

After deploy, verify liveness (`/healthz`) and readiness (`/readyz`) as in [Health checks](#health-checks).

Private packages: authenticate with a GitHub token that can `read:packages` (CI uses `GITHUB_TOKEN` with `packages: write` on push).

The current Dockerfile:

- uses a multi-stage build
- produces a static binary at `/usr/local/bin/shopanda`
- runs as non-root user `appuser`
- exposes port `8080`
- includes migrations, theme files, config, and OpenAPI assets
- performs container **liveness** checks against `/healthz` (not `/readyz`)

### Run a single container

```bash
docker run --rm \
  -p 8080:8080 \
  --env-file .env \
  shopanda
```

Run one-off tasks with the same image:

```bash
docker run --rm --env-file .env shopanda migrate
docker run --rm --env-file .env shopanda seed
docker run --rm --env-file .env shopanda setup
```

### Use Docker Compose for a fuller deployment

Current repository behavior:

- `docker-compose.yml` starts `app`, `worker`, `scheduler`, and `postgres` by default
- `mailpit` is available behind the `dev` profile (local SMTP capture at http://localhost:8025)
- `meilisearch` is available behind the `search` profile
- Postgres data persists in the `pgdata` named volume

Quick start:

```bash
cp .env.example .env
docker compose up -d
docker compose run --rm app migrate
docker compose run --rm app seed
```

For local email testing with Mailpit:

```bash
docker compose --profile dev up -d
```

### Minimum production checklist (Compose)

Before relying on the store in production:

1. Postgres migrated (`docker compose run --rm app migrate` or init job).
2. `app` (HTTP) reachable behind TLS termination.
3. `worker` running continuously (order emails, password reset, async jobs).
4. `scheduler` running continuously (cache cleanup and recurring tasks).
5. SMTP configured in `.env` and verified (send a test email from `/admin/settings`).
6. `SHOPANDA_SERVER_PUBLIC_BASE_URL` set to the public storefront URL.
7. Stripe keys + webhook secret if card payments are enabled.

### Persist uploaded media

If you use local media storage in containers, add a volume for `/app/public/media`. The default compose file does not yet mount that path.

Recommended override:

```yaml
services:
  app:
    volumes:
      - media:/app/public/media

volumes:
  media:
```

Without this, uploaded files disappear when the application container is replaced.

### Background processes in Compose

The default compose file already runs background processes as separate services from the same image:

| Service | Command | Role |
| --- | --- | --- |
| `app` | `serve` | HTTP server plus embedded job worker (admin, storefront, REST API) |
| `worker` | `worker` | Dedicated job consumer (transactional email, cache cleanup) |
| `scheduler` | `scheduler` | Cron dispatcher (enqueues recurring jobs) |

All three share the same `.env` and database connection settings.

The `serve` command starts an embedded worker goroutine in the same process as HTTP (`cmd/api/main.go`). The dedicated `worker` service runs additional job consumers from the same image. Both use `FOR UPDATE SKIP LOCKED` on the Postgres queue, so concurrent consumers are safe.

Scale `app` horizontally for HTTP capacity; each replica also runs an embedded worker, so background job load grows with web replicas. Scale the `worker` service independently when email or async throughput needs more capacity. Keep a single `scheduler` replica.

**Emails require job workers and SMTP.** Orders complete in `app`, but confirmation and password-reset emails are enqueued and delivered by job workers (`app`'s embedded worker and/or the dedicated `worker` service). Configure SMTP in `.env` and verify delivery from `/admin/settings`.

For bare-metal or custom orchestration without Compose, run the same three commands as separate processes — see [Example systemd units](#example-systemd-units).

## Deploy To Cloud Platforms

These examples are intentionally minimal and use the current runtime contract: one image, commands for `serve`, `worker`, and `scheduler`, plus PostgreSQL.

### Railway

Use Railway when you want the least amount of infrastructure work.

Recommended layout:

- web service running `shopanda serve`
- worker service running `shopanda worker`
- scheduler service running `shopanda scheduler`
- Railway PostgreSQL plugin or managed external Postgres

Set at least:

- `SHOPANDA_AUTH_JWT_SECRET`
- `SHOPANDA_SERVER_PUBLIC_BASE_URL`
- database connection settings or `DATABASE_URL`
- optional `SHOPANDA_SEED_ADMIN_PASSWORD` if you want `setup` to create the seeded admin user on first boot

After deploy, run:

```bash
shopanda setup
```

in a one-off Railway shell or job.

### Fly.io

Use separate process groups for web, worker, and scheduler.

Example `fly.toml` shape:

```toml
app = "shopanda"
primary_region = "ams"

[build]
  dockerfile = "Dockerfile"

[env]
  SHOPANDA_SERVER_PORT = "8080"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = false
  auto_start_machines = true

[[vm]]
  memory = "512mb"
  cpu_kind = "shared"
  cpus = 1
```

If you split worker and scheduler into separate Fly apps or process groups, keep the same environment and image, and change only the command.

### DigitalOcean App Platform

Use one web component and optional worker components from the same repository.

Suggested process layout:

- web component: `serve`
- worker component: `worker`
- scheduler component: `scheduler`

Back it with managed PostgreSQL and inject the same core secrets used elsewhere.

## Deploy On Bare Metal

Use bare metal or a VPS when you want full control and already have a reverse proxy and PostgreSQL available.

Use a dedicated unprivileged service account for the running processes. Root should install files and manage services, but `shopanda serve`, `shopanda worker`, and `shopanda scheduler` should run as a non-root user such as `shopanda`.

For the examples below, assume your deploy or checkout directory is `/srv/shopanda`. Replace that path with your actual working directory. The simplest setup is to keep the binary, migrations, themes, and `configs/config.yaml` in that working directory and keep only secrets outside it.

### Create the service user and secrets directory

Example Linux layout:

```bash
sudo groupadd --system shopanda
sudo useradd --system --gid shopanda --home-dir /srv/shopanda --shell /usr/sbin/nologin shopanda
sudo install -d -o root -g shopanda -m 0750 /etc/shopanda
sudo chown -R shopanda:shopanda /srv/shopanda
```

### Build the binary in place

```bash
cd /srv/shopanda
go build -o shopanda ./cmd/api
```

Do not separate the binary from its working directory unless you deliberately want a packaged layout. The current runtime expects files relative to where it starts, including:

- `migrations/` for `setup` and `migrate`
- `openapi.yaml` for `/docs/openapi.yaml`
- `themes/` if `SHOPANDA_FRONTEND_ENABLED=true`

### Create the config and environment files

Keep non-secret config in the working directory:

```bash
cd /srv/shopanda
cp ./configs/config.example.yaml ./configs/config.yaml
sudoedit /srv/shopanda/configs/config.yaml
```

For environment variables used by the services, keep them outside the repository in `/etc/shopanda/shopanda.env`:

```bash
sudo cp ./.env.example /etc/shopanda/shopanda.env
sudo chown root:shopanda /etc/shopanda/shopanda.env
sudo chmod 0640 /etc/shopanda/shopanda.env
sudoedit /etc/shopanda/shopanda.env
```

Keep the file in plain `KEY=value` form with no spaces around `=`. If a value contains shell-sensitive characters, quote it, for example `SHOPANDA_DATABASE_PASSWORD='s%v2M+aa'`.

Security note: keep `configs/config.yaml` for non-secret configuration only. Put credentials, API keys, webhook secrets, SMTP passwords, JWT secrets, and other sensitive values only in `/etc/shopanda/shopanda.env` or another `0640`-protected secret store. Preserve the ownership and permission shown here for `shopanda.env`: `0640` owned by `root:shopanda`.

If you already ran `./install.sh`, you can copy the generated repo-root `.env` into `/etc/shopanda/shopanda.env` as the starting point instead of `.env.example`. After copying it, delete the production `.env` from the repository checkout so secrets are not left beside the codebase or one mistaken commit away from exposure. The service setup below reads `/etc/shopanda/shopanda.env`, not the repo-root `.env`.

Use the same `/etc/shopanda/shopanda.env` for `serve`, `worker`, and `scheduler` unless you intentionally need per-process overrides. Separate env files are usually unnecessary.

The `shopanda` service account must be able to read `/etc/shopanda/shopanda.env` and write to `/srv/shopanda/public/media` when local media storage is enabled.

### First-time setup

```bash
sudo -u shopanda sh -c 'cd /srv/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && exec ./shopanda setup'
```

If you prefer explicit steps:

```bash
sudo -u shopanda sh -c 'cd /srv/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && exec ./shopanda migrate'
sudo -u shopanda sh -c 'cd /srv/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && exec ./shopanda seed'
```

### Quick manual background run

Use this only for quick testing or temporary bring-up. For long-lived production processes, prefer the service manager examples below.

```bash
sudo -u shopanda sh -c 'cd /srv/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && nohup ./shopanda serve >>/tmp/shopanda-web.log 2>&1 &'
sudo -u shopanda sh -c 'cd /srv/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && nohup ./shopanda worker >>/tmp/shopanda-worker.log 2>&1 &'
sudo -u shopanda sh -c 'cd /srv/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && nohup ./shopanda scheduler >>/tmp/shopanda-scheduler.log 2>&1 &'
```

These commands return your terminal immediately and write logs to `/tmp/`. For long-lived services, prefer `systemd` so logs go to the journal.

### Example systemd units

Save these as:

- `/etc/systemd/system/shopanda-web.service`
- `/etc/systemd/system/shopanda-worker.service`
- `/etc/systemd/system/shopanda-scheduler.service`

Web service:

```ini
[Unit]
Description=Shopanda Web
After=network.target postgresql.service

[Service]
WorkingDirectory=/srv/shopanda
ExecStart=/srv/shopanda/shopanda serve
Restart=always
EnvironmentFile=/etc/shopanda/shopanda.env
User=shopanda
Group=shopanda

[Install]
WantedBy=multi-user.target
```

Worker service:

```ini
[Unit]
Description=Shopanda Worker
After=network.target postgresql.service

[Service]
WorkingDirectory=/srv/shopanda
ExecStart=/srv/shopanda/shopanda worker
Restart=always
EnvironmentFile=/etc/shopanda/shopanda.env
User=shopanda
Group=shopanda

[Install]
WantedBy=multi-user.target
```

Scheduler service:

```ini
[Unit]
Description=Shopanda Scheduler
After=network.target postgresql.service

[Service]
WorkingDirectory=/srv/shopanda
ExecStart=/srv/shopanda/shopanda scheduler
Restart=always
EnvironmentFile=/etc/shopanda/shopanda.env
User=shopanda
Group=shopanda

[Install]
WantedBy=multi-user.target
```

### Enable, start, and debug the services

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now shopanda-web.service shopanda-worker.service shopanda-scheduler.service
sudo systemctl status shopanda-web.service
sudo systemctl status shopanda-worker.service
sudo systemctl status shopanda-scheduler.service
sudo journalctl -u shopanda-web.service -f
sudo journalctl -u shopanda-worker.service -f
sudo journalctl -u shopanda-scheduler.service -f
```

When you change `/etc/shopanda/shopanda.env` or the unit files, restart the affected services:

```bash
sudo systemctl restart shopanda-web.service
sudo systemctl restart shopanda-worker.service
sudo systemctl restart shopanda-scheduler.service
```

### Short FreeBSD rc.d example

If you run Shopanda on FreeBSD, use the same working-directory approach but keep the service env file under `/usr/local/etc/shopanda.env`.

FreeBSD commonly stores third-party service scripts and related local configuration under `/usr/local/etc`, which is why the `shopanda_web` example uses `/usr/local/etc/rc.d/shopanda_web` and `/usr/local/etc/shopanda.env` even though the working directory remains `/srv/shopanda`. On Linux, the matching examples use `/etc/shopanda/shopanda.env` with the same working-directory layout.

Set the service flags in `/etc/rc.conf`:

```sh
shopanda_web_enable="YES"
shopanda_web_user="shopanda"
```

Save this as `/usr/local/etc/rc.d/shopanda_web`:

```sh
#!/bin/sh

# PROVIDE: shopanda_web
# REQUIRE: LOGIN postgresql
# KEYWORD: shutdown

. /etc/rc.subr

name="shopanda_web"
rcvar="${name}_enable"

: ${shopanda_web_enable:="NO"}
: ${shopanda_web_user:="shopanda"}

pidfile="/var/run/${name}.pid"
procname="/usr/sbin/daemon"
command="/usr/sbin/daemon"
command_args="-f -P ${pidfile} -u ${shopanda_web_user} /bin/sh -c 'cd /srv/shopanda && set -a && . /usr/local/etc/shopanda.env && set +a && exec /srv/shopanda/shopanda serve'"

load_rc_config "$name"
run_rc_command "$1"
```

Then enable and start it:

```sh
sudo service shopanda_web enable
sudo service shopanda_web start
sudo service shopanda_web status
sudo tail -f /var/log/messages
```

For `worker` and `scheduler`, duplicate the script as `shopanda_worker` and `shopanda_scheduler` and change only the service name and final Shopanda command.

## Configure TLS And HTTPS

### Caddy

Use Caddy when you want the simplest automatic TLS setup.

```caddyfile
shop.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

### Nginx

Use Nginx when you already standardize on it for the rest of your infrastructure.

```nginx
server {
    listen 80;
    server_name shop.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Pair Nginx with Let's Encrypt or your normal certificate automation.

## Operate PostgreSQL Safely

### Set up the database

Requirements:

- PostgreSQL
- a dedicated application database
- a dedicated application user
- network access from the Shopanda processes to PostgreSQL

Use either individual DB environment variables or `DATABASE_URL`.

### Consider connection pooling

For larger deployments, place PgBouncer between Shopanda and PostgreSQL, especially when running multiple web and worker instances.

### Back up the database

Simple logical backup:

```bash
pg_dump "$DATABASE_URL" > shopanda-$(date +%F).sql
```

Restore example:

```bash
psql "$DATABASE_URL" < shopanda-2026-04-22.sql
```

### Back up media files

If you use local media storage, back up the media path as well:

```bash
tar -czf shopanda-media-$(date +%F).tar.gz ./public/media
```

If you use S3-compatible storage, rely on bucket-level lifecycle and backup policies instead.

### Migration filename policy

Migrations under `migrations/` are plain, forward-only SQL files applied in filename order — there is no golang-migrate/goose (or similar) tool and **no down migrations**. Rollback means **restore from a backup** (see above), not reversing a migration.

Naming rule, enforced by a CI unit test (`internal/platform/migrate` — runs in `CI / unit`, no database required):

- every `*.sql` file must match `^([0-9]+)_.*\.sql$` — a numeric prefix, then `_`, then a description
- every numeric prefix must use the **same digit width** as the rest (currently 3 digits, e.g. `064_`) — migrations are applied in plain lexicographic filename order, so a mismatched width (e.g. `64_` next to `065_`) sorts and runs out of numeric order
- numeric prefixes are normalized as integers (leading zeros stripped: `007` and `7` collide) and must be **unique** across all files
- one historical exception is allowlisted by exact filename: `025_add_cart_merged_guest_id.sql` and `025_create_invoices.sql` both predate this check and share prefix `25`
- **never rename or delete** either of those two files — their filenames are recorded verbatim in every deployed `schema_migrations` table; renaming desyncs tracking and makes Shopanda re-attempt (and fail) an already-applied migration
- do **not** add new allowlist entries to resolve a new collision — renumber the new migration file instead; the check fails the build if a new file reuses an existing prefix, if an allowlisted filename is renamed/removed, or if a third file is added at an allowlisted prefix

## Monitor The Deployment

### Health checks

Distinguish **liveness** from **readiness**:

| Probe | Endpoint | Meaning |
| --- | --- | --- |
| Liveness | `GET`/`HEAD` `/healthz` | Process is up (static 200, `Cache-Control: no-store`). Docker image `HEALTHCHECK` uses this. Mounted outside store/auth middleware. |
| Readiness | `GET`/`HEAD` `/readyz` | Database ping succeeds within ~2s → 200; else **503**. Dedicated per-IP rate limit (defaults: same as `rate_limit.default`). Prefer restricting probe exposure via network policy / internal listener — do not leave an unauthenticated DB-ping endpoint fully public without a gateway. |

```bash
curl -f http://127.0.0.1:8080/healthz
curl -f http://127.0.0.1:8080/readyz
```

Do **not** point the container `HEALTHCHECK` at `/readyz` — a temporary DB blip would restart the process instead of only stopping new traffic.

### Logs

Structured JSON logs are the default:

- set `SHOPANDA_LOG_FORMAT=json` for machine-readable logs
- set `SHOPANDA_LOG_LEVEL` appropriately for the environment

This makes Shopanda suitable for log aggregation systems such as Loki, Datadog, ELK, or platform-native logging.

### Operator checks after deploy

After every deploy, verify:

1. `/healthz` returns success (liveness).
2. `/readyz` returns success (DB reachable).
3. `/docs` opens.
4. `/admin` loads.
5. a worker is running if you depend on async email or jobs.
6. a scheduler is running if you depend on recurring tasks.

## Related Guides

- [Merchant Guide](MERCHANT.md)
- [README](../../README.md)
- [Configuration Reference](../../configs/config.example.yaml)