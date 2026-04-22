# Deployment & Operations Runbook

This runbook is the short operational reference for running Shopanda in development or production.

For a fuller deployment guide, including Docker and environment variable details, see `docs/guides/DEPLOYMENT.md`.

## Prerequisites

- Go toolchain if you are building from source
- PostgreSQL
- a configured JWT secret

## 1. Clone and build

```bash
git clone <repo>
cd shopanda
go build -o app ./cmd/api
```

If you already have a compiled binary, you can skip the build step.

## 2. Configure the application

You can configure Shopanda with environment variables, `configs/config.yaml`, or both.

Useful references already in the repo:

- `.env.example`
- `configs/config.example.yaml`

Common local setup:

```bash
cp .env.example .env
```

At minimum, set:

- `SHOPANDA_AUTH_JWT_SECRET`
- `SHOPANDA_SERVER_PUBLIC_BASE_URL`
- database settings via either `DATABASE_URL` or the `SHOPANDA_DATABASE_*` variables

Production note:

- `.env` is mainly a local-development convenience
- in production, prefer a real `configs/config.yaml` or environment injection from your process manager or container platform

## 3. First-time setup

Run the built-in setup command on a fresh environment:

```bash
./app setup
```

`setup` checks database connectivity, runs migrations, and runs the default seeders.

If you want the seeded admin account, set this before `setup` or `seed`:

```bash
export SHOPANDA_SEED_ADMIN_PASSWORD=change-me-now
```

You can still run the lower-level commands directly when needed:

```bash
./app migrate
./app seed
```

## 4. Run the services

### HTTP server

```bash
./app serve
```

### Background worker

```bash
./app worker
```

### Scheduler

Run this when you want cron-style recurring jobs enabled:

```bash
./app scheduler
```

## 5. Verify the deployment

### Health check

```bash
curl http://localhost:8080/healthz
```

### Useful URLs

- Admin: `http://localhost:8080/admin`
- API docs UI: `http://localhost:8080/docs`
- OpenAPI spec: `http://localhost:8080/docs/openapi.yaml`

### Logs

Shopanda logs to stdout/stderr. Use the log viewer for your runtime:

- `docker compose logs -f app`
- `docker compose logs -f worker`
- `journalctl -fu <service-name>`
- your process manager's log viewer

## 6. Common operations

### Show command help

```bash
./app help
```

### Reindex search

```bash
./app search:reindex
```

### Export configuration

Exports YAML to stdout, so redirect it to a file when needed:

```bash
./app config:export > config.snapshot.yaml
```

### Import configuration

```bash
./app config:import config.snapshot.yaml
```

## 7. Scaling

### API

- run multiple `serve` instances
- put them behind a load balancer or reverse proxy
- keep `SHOPANDA_SERVER_PUBLIC_BASE_URL` aligned with the public entrypoint

### Worker

- run multiple `worker` processes when queue throughput increases
- keep only one scheduler unless you have explicit coordination for recurring jobs

## 8. Backup

### Database

```bash
pg_dump "$DATABASE_URL" > shopanda-backup.sql
```

If you do not use `DATABASE_URL`, use the equivalent `pg_dump` flags for your PostgreSQL host, user, and database.

### Media

- if `SHOPANDA_MEDIA_STORAGE=local`, back up the local media directory, typically `./public/media`
- if you use an object-store backend, rely on the bucket backup/retention policy for that service

## 9. Troubleshooting

### App will not start

- verify `SHOPANDA_AUTH_JWT_SECRET` is set
- verify database settings are correct
- verify the config source you edited is the one the app is loading

### Jobs are not processing

- confirm at least one `worker` process is running
- check worker logs for handler or database errors

### Scheduled jobs are not running

- confirm the `scheduler` process is running
- verify you did not accidentally start multiple conflicting schedulers

### Payments are not updating

- verify the provider webhook endpoint is reachable
- verify the provider secret is configured correctly
- check the HTTP and worker logs for webhook handling errors

## 10. Production checklist

- [ ] PostgreSQL is secured and backed up
- [ ] `SHOPANDA_AUTH_JWT_SECRET` is set to a strong secret
- [ ] HTTPS is enabled at the edge or reverse proxy
- [ ] at least one worker is running
- [ ] scheduler coverage is in place if recurring jobs are required
- [ ] logs and health checks are monitored
- [ ] media storage and database backup procedures are tested

## 11. Minimal command set

Fresh environment:

```bash
./app setup
./app serve
./app worker
```

That is the smallest complete operating baseline for most deployments.
