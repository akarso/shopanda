# Runtime Modes — Development vs Production

## Purpose

Shopanda is a single binary with multiple commands. Some capabilities depend on background processing. This document defines how those processes should run in **development** (minimal friction) versus **production** (reliable, observable).

It is the operational contract for Phase 4 Track D (PR-430, PR-431).

---

## Process Model

| Command | Role | Required for |
| --- | --- | --- |
| `serve` | HTTP server (admin, storefront, REST API) | All interactive use |
| `worker` | Job consumer (`email.send`, cache cleanup, …) | Transactional email, async side effects |
| `scheduler` | Cron dispatcher (enqueues periodic jobs) | Cache cleanup schedule, future recurring tasks |

Today:

- **Notification emails** are enqueued on domain events and delivered only when a worker is running.
- **Cache cleanup** is scheduled by the scheduler and executed by the worker.
- **Checkout and order creation** run synchronously in `serve`; they do not require a worker to complete.

If `serve` runs without an embedded worker (standalone `worker` not started), the storefront and admin remain usable, but **emails will not send**. `serve` embeds a worker by default; use separate `worker`/`scheduler` services in production (PR-431).

---

## Development Mode (PR-430)

**Goal:** one command for local hacking — `./app dev`.

| Aspect | Development default |
| --- | --- |
| Processes | Single OS process: HTTP server + embedded worker loop |
| Scheduler | Embedded in `app dev` by default (`dev.embed_scheduler: true`); disable via config or `SHOPANDA_DEV_EMBED_SCHEDULER=false` |
| Mail | Mailpit via `docker compose --profile dev` (SMTP `localhost:1025`, UI `localhost:8025`) |
| Search | Postgres FTS (no extra service) |
| Cache / queue | Postgres-backed (no Redis/RabbitMQ) |
| Config | `.env` from `install.sh` or `.env.example` |

### Commands

```bash
./app dev          # recommended — HTTP + worker + scheduler
./app serve        # HTTP + embedded worker only (no scheduler)
```

For scheduler parity without `dev`, run a second terminal:

```bash
./app scheduler
```

Or use Docker Compose (Postgres only today — **worker is not a compose service yet**; PR-431 adds it for production profiles).

---

## Production Mode (target after PR-431)

**Goal:** explicit, restart-safe process separation suitable for Docker, systemd, Fly.io, or Kubernetes.

| Aspect | Production default |
| --- | --- |
| Processes | **Three** replicas or services from the same image: `serve`, `worker`, `scheduler` |
| Scaling | Scale `serve` horizontally; typically **one** `scheduler`; scale `worker` with job volume |
| Mail | Real SMTP (env-configured) |
| Search | `postgres` (default) or `meilisearch` when `SHOPANDA_SEARCH_ENGINE=meilisearch` and search profile/service is up |
| Cache | `postgres` (default) or `redis` when `cache.driver=redis` (PR-414) |
| Queue | `postgres` (default), `redis` (PR-415), or `rabbitmq` (PR-416) — one active `queue.driver` only |
| Secrets | Env file or secret manager — never committed config |

### Minimum production checklist

1. Postgres migrated (`app migrate` or init job).
2. `serve` behind TLS termination.
3. `worker` running continuously.
4. `scheduler` running continuously (single instance).
5. SMTP configured and verified (order confirmation, password reset, email change).
6. `SHOPANDA_SERVER_PUBLIC_BASE_URL` set to the public storefront URL (email links, SEO).
7. Stripe keys + webhook secret if card payments are enabled.

### Docker Compose (production layout — PR-431)

```text
services:
  app:        command: serve
  worker:     command: worker      # same image, no published ports
  scheduler:  command: scheduler   # same image, single replica
  postgres:   ...
```

Optional profiles: `dev` (mailpit), `search` (meilisearch), `queue` (rabbitmq — PR-416).

---

## When Each Mode Applies

| Scenario | Mode | Commands |
| --- | --- | --- |
| Local theme / handler work | Development | `app dev` |
| CI integration tests | Development | `serve` + `worker` in test harness |
| Staging / production | Production | `serve`, `worker`, `scheduler` as separate services |
| Headless API-only deployment | Production | Same as production; storefront templates optional |

---

## Failure Signatures

| Symptom | Likely cause |
| --- | --- |
| Orders complete but no emails | `worker` not running or SMTP misconfigured |
| Cache grows without bound | `scheduler` + `worker` not running |
| Meilisearch index empty | `search.engine=meilisearch` but engine not up; run `app search:reindex` after engine is ready |
| Stripe payments stuck | Webhook not reaching `serve` or secret mismatch |

---

## Relation to Plugin Tiers (Phase 4 Track B)

Optional backends (Meilisearch, Redis, RabbitMQ, Stripe, S3) are **configuration-gated**. Development should work with **zero** optional services. Production enables them via env + compose profiles or core plugins (PR-410–416) without changing the process model above.
