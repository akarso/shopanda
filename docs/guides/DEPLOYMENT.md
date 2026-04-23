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

### 4. Run first-time setup

```bash
./shopanda setup
```

The setup command:

- checks database connectivity
- runs migrations
- runs default seeders unless `--skip-seed` is used
- prints store, admin API, and docs URLs

If you prefer explicit commands instead of `setup`:

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
open http://localhost:8080/docs
open http://localhost:8080/admin
```

Live endpoints in the current application:

- health: `/healthz`
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

### Database

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `DATABASE_URL` | Optional alternative | none | Full PostgreSQL DSN; overrides individual DB fields |
| `SHOPANDA_DATABASE_HOST` | Yes unless `DATABASE_URL` is set | `localhost` | PostgreSQL host |
| `SHOPANDA_DATABASE_PORT` | No | `5432` | PostgreSQL port |
| `SHOPANDA_DATABASE_USER` | Yes unless `DATABASE_URL` is set | `shopanda` | PostgreSQL user |
| `SHOPANDA_DATABASE_PASSWORD` | Yes unless `DATABASE_URL` is set | empty | PostgreSQL password |
| `SHOPANDA_DATABASE_NAME` | Yes unless `DATABASE_URL` is set | `shopanda` | PostgreSQL database name |
| `SHOPANDA_DATABASE_SSLMODE` | No | `disable` | PostgreSQL SSL mode |

### Logging

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_LOG_LEVEL` | No | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `SHOPANDA_LOG_FORMAT` | No | `json` | Log format: `json` or `text` |

### Authentication

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_AUTH_JWT_SECRET` | Yes | none | JWT signing secret; application refuses to start without it |
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
| `SHOPANDA_RATE_LIMIT_ENABLED` | No | `false` | Enable request rate limiting |
| `SHOPANDA_RATE_LIMIT_DEFAULT_RATE` | No | `10` | Default tokens per second |
| `SHOPANDA_RATE_LIMIT_DEFAULT_BURST` | No | `20` | Default burst size |

### Seeding

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_SEED_ADMIN_PASSWORD` | Required for `setup` or `seed` when the admin account does not yet exist | none | Password for seeded admin user `admin@example.com` |

### Development and Testing

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `SHOPANDA_DEV_MODE` | No | empty | Enables development-only behavior |
| `SHOPANDA_TEST_DSN` | No | empty | PostgreSQL DSN used by integration tests |

### Security notes

- never reuse the sample database password in real environments
- treat `SHOPANDA_AUTH_JWT_SECRET` like a production credential; rotate it if leaked
- prefer shell- or platform-injected secrets over committing secrets into YAML
- if you expose Meilisearch to the internet, set a real master key instead of the local-dev default

## Deploy With Docker

### Build the image

```bash
docker build -t shopanda .
```

The current Dockerfile:

- uses a multi-stage build
- produces a static binary at `/usr/local/bin/shopanda`
- runs as non-root user `appuser`
- exposes port `8080`
- includes migrations, theme files, config, and OpenAPI assets
- performs health checks against `/healthz`

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

- `docker-compose.yml` starts `app` and `postgres` by default
- `mailpit` is available behind the `dev` profile
- `meilisearch` is available behind the `search` profile
- Postgres data persists in the `pgdata` named volume

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

### Run worker and scheduler containers

For production, run background processes separately from the web server even though they use the same image.

Example compose override:

```yaml
services:
  app:
    command: serve

  worker:
    image: shopanda
    command: worker
    env_file:
      - .env
    depends_on:
      postgres:
        condition: service_healthy

  scheduler:
    image: shopanda
    command: scheduler
    env_file:
      - .env
    depends_on:
      postgres:
        condition: service_healthy
```

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
- `SHOPANDA_SEED_ADMIN_PASSWORD` for first setup

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

### Create the service user and directories

Example Linux layout:

```bash
sudo groupadd --system shopanda
sudo useradd --system --gid shopanda --home-dir /opt/shopanda --create-home --shell /usr/sbin/nologin shopanda
sudo install -d -o shopanda -g shopanda -m 0755 /opt/shopanda /opt/shopanda/configs /opt/shopanda/public /opt/shopanda/public/media
sudo install -d -o root -g shopanda -m 0750 /etc/shopanda
sudo install -d -o shopanda -g shopanda -m 0750 /var/log/shopanda
```

### Build and install the binary

```bash
go build -o shopanda ./cmd/api
sudo install -o root -g shopanda -m 0755 ./shopanda /opt/shopanda/shopanda
```

Do not install only the binary. The current runtime still expects some files relative to its working directory:

- `migrations/` for `setup` and `migrate`
- `openapi.yaml` for `/docs/openapi.yaml`
- `themes/` if `SHOPANDA_FRONTEND_ENABLED=true`

Install those runtime assets into `/opt/shopanda` too:

```bash
sudo cp -R ./migrations /opt/shopanda/migrations
sudo cp ./openapi.yaml /opt/shopanda/openapi.yaml
sudo cp -R ./themes /opt/shopanda/themes
```

### Create the config and environment files

The binary expects a real config file in its working directory. Start from the example file:

```bash
sudo install -o root -g shopanda -m 0644 ./configs/config.example.yaml /opt/shopanda/configs/config.yaml
sudoedit /opt/shopanda/configs/config.yaml
```

For environment variables used by the services, keep them outside the repository in `/etc/shopanda/shopanda.env`:

```bash
sudo cp ./.env.example /etc/shopanda/shopanda.env
sudo chown root:shopanda /etc/shopanda/shopanda.env
sudo chmod 0640 /etc/shopanda/shopanda.env
sudoedit /etc/shopanda/shopanda.env
```

Keep the file in plain `KEY=value` form with no spaces around `=`. If a value contains shell-sensitive characters, quote it, for example `SHOPANDA_DATABASE_PASSWORD='s%v2M+aa'`.

Security note: `/opt/shopanda/configs/config.yaml` is installed with mode `0644`, so it is world-readable and must not contain secrets. Keep credentials, API keys, webhook secrets, SMTP passwords, and other sensitive values only in `/etc/shopanda/shopanda.env` or another `0640`-protected secret store. Preserve the ownership and permissions shown here: `config.yaml` at `0644`, and `shopanda.env` at `0640` owned by `root:shopanda`.

If you already ran `./install.sh`, you can copy the generated repo-root `.env` into `/etc/shopanda/shopanda.env` as the starting point instead of `.env.example`. After copying it, delete the production `.env` from the repository checkout so secrets are not left beside the codebase or one mistaken commit away from exposure. The service setup below reads `/etc/shopanda/shopanda.env`, not the repo-root `.env`.

The `shopanda` service account must be able to read `/etc/shopanda/shopanda.env` and write to `/opt/shopanda/public/media` when local media storage is enabled.

### First-time setup

```bash
sudo -u shopanda sh -c 'cd /opt/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && exec ./shopanda setup'
```

If you prefer explicit steps:

```bash
sudo -u shopanda sh -c 'cd /opt/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && exec ./shopanda migrate'
sudo -u shopanda sh -c 'cd /opt/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && exec ./shopanda seed'
```

### Quick manual background run

Use this only for quick testing or temporary bring-up. For long-lived production processes, prefer the service manager examples below.

```bash
sudo -u shopanda sh -c 'cd /opt/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && nohup ./shopanda serve >>/var/log/shopanda/web.log 2>&1 &'
sudo -u shopanda sh -c 'cd /opt/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && nohup ./shopanda worker >>/var/log/shopanda/worker.log 2>&1 &'
sudo -u shopanda sh -c 'cd /opt/shopanda && set -a && . /etc/shopanda/shopanda.env && set +a && nohup ./shopanda scheduler >>/var/log/shopanda/scheduler.log 2>&1 &'
```

These commands return your terminal immediately and write logs to `/var/log/shopanda/`.

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
WorkingDirectory=/opt/shopanda
ExecStart=/opt/shopanda/shopanda serve
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
WorkingDirectory=/opt/shopanda
ExecStart=/opt/shopanda/shopanda worker
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
WorkingDirectory=/opt/shopanda
ExecStart=/opt/shopanda/shopanda scheduler
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

If you run Shopanda on FreeBSD, use the same `/opt/shopanda` layout but keep the service env file under `/usr/local/etc/shopanda.env`.

FreeBSD commonly stores third-party service scripts and related local configuration under `/usr/local/etc`, which is why the `shopanda_web` example uses `/usr/local/etc/rc.d/shopanda_web` and `/usr/local/etc/shopanda.env` even though the deploy root remains `/opt/shopanda`. On Linux, the matching examples use `/etc/shopanda/shopanda.env` with the same `/opt/shopanda` application layout.

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
command_args="-f -P ${pidfile} -u ${shopanda_web_user} /bin/sh -c 'cd /opt/shopanda && set -a && . /usr/local/etc/shopanda.env && set +a && exec /opt/shopanda/shopanda serve'"

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

## Monitor The Deployment

### Health checks

Use:

```bash
curl -f http://127.0.0.1:8080/healthz
```

The Docker image already uses `/healthz` for its built-in container health check.

### Logs

Structured JSON logs are the default:

- set `SHOPANDA_LOG_FORMAT=json` for machine-readable logs
- set `SHOPANDA_LOG_LEVEL` appropriately for the environment

This makes Shopanda suitable for log aggregation systems such as Loki, Datadog, ELK, or platform-native logging.

### Operator checks after deploy

After every deploy, verify:

1. `/healthz` returns success.
2. `/docs` opens.
3. `/admin` loads.
4. a worker is running if you depend on async email or jobs.
5. a scheduler is running if you depend on recurring tasks.

## Related Guides

- [Merchant Guide](MERCHANT.md)
- [README](../../README.md)
- [Configuration Reference](../../configs/config.example.yaml)