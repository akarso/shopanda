# 🐳 DevOps & Setup — Specification

## 1. Overview

Handles:

* environment configuration templates
* containerized builds and deployment
* one-command local setup
* health verification

Design goals:

* zero-to-running in under 5 minutes
* no manual database setup required
* production-ready Docker image
* developer and non-developer friendly

---

## 2. Environment Configuration (PR-200)

---

### 2.1 `.env.example`

Annotated template with all supported variables:

```env
# === Server ===
SHOPANDA_SERVER_PORT=8080
SHOPANDA_SERVER_HOST=0.0.0.0

# === Database ===
SHOPANDA_DATABASE_HOST=localhost
SHOPANDA_DATABASE_PORT=5432
SHOPANDA_DATABASE_NAME=shopanda
SHOPANDA_DATABASE_USER=shopanda
SHOPANDA_DATABASE_PASSWORD=changeme
SHOPANDA_DATABASE_SSLMODE=disable

# === Auth ===
SHOPANDA_AUTH_JWT_SECRET=generate-a-secure-secret-here

# === Mail (SMTP) ===
SHOPANDA_MAIL_SMTP_HOST=
SHOPANDA_MAIL_SMTP_PORT=587
SHOPANDA_MAIL_SMTP_USER=
SHOPANDA_MAIL_SMTP_PASSWORD=
SHOPANDA_MAIL_FROM_ADDRESS=noreply@example.com
SHOPANDA_MAIL_FROM_NAME=My Store

# === Media ===
SHOPANDA_MEDIA_STORAGE=local
SHOPANDA_MEDIA_LOCAL_PATH=./public/media
SHOPANDA_MEDIA_BASE_URL=/media

# === Frontend ===
SHOPANDA_FRONTEND_ENABLED=false

# === Rate Limiting ===
SHOPANDA_RATELIMIT_ENABLED=true
SHOPANDA_RATELIMIT_REQUESTS_PER_SECOND=10

# === Seeding ===
SHOPANDA_SEED_ADMIN_EMAIL=admin@example.com
SHOPANDA_SEED_ADMIN_PASSWORD=changeme
```

---

### 2.2 `configs/config.example.yaml`

Same content as YAML with inline comments explaining each section.

---

## 3. Dockerfile (PR-201)

---

### Multi-stage build

```dockerfile
# Stage 1: Build
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/api

# Stage 2: Runtime
FROM alpine:3.19
RUN adduser -D -u 1000 appuser
COPY --from=builder /app /usr/local/bin/app
COPY migrations/ /app/migrations/
COPY configs/config.example.yaml /app/config.yaml
COPY openapi.yaml /app/openapi.yaml
USER appuser
EXPOSE 8080
HEALTHCHECK CMD wget -qO- http://localhost:8080/api/v1/health || exit 1
ENTRYPOINT ["app"]
CMD ["serve"]
```

---

### Requirements

* Non-root user
* Health check built-in
* Minimal image size (< 30MB)
* Config and migrations bundled
* Supports `serve`, `migrate`, `seed` as CMD overrides

---

## 4. Docker Compose (PR-202)

---

### Services

```yaml
services:
  app:
    build: .
    ports:
      - "8080:8080"
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: shopanda
      POSTGRES_USER: shopanda
      POSTGRES_PASSWORD: changeme
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: pg_isready -U shopanda

  # Optional services
  meilisearch:
    image: getmeili/meilisearch:v1.6
    profiles: ["search"]

  mailpit:
    image: axllent/mailpit
    ports:
      - "8025:8025"
    profiles: ["dev"]

volumes:
  pgdata:
```

---

### Profiles

* Default: `app` + `postgres`
* `dev`: adds `mailpit` for email testing
* `search`: adds `meilisearch`

---

## 5. Setup CLI Command (PR-203)

---

### Usage

```bash
app setup
```

---

### Flow

1. Check Postgres connectivity
2. Run pending migrations
3. Check if admin user exists — if not, prompt or use env
4. Run seeders (idempotent)
5. Verify health endpoint
6. Print summary: URL, admin credentials, next steps

---

### Flags

```text
--non-interactive    Use env vars only, no prompts
--skip-seed          Skip seeding step
--verbose            Print detailed progress
```

---

### Output

```text
✓ Database connected
✓ 35 migrations applied
✓ Admin user created (admin@example.com)
✓ Catalog seeded (3 products, 2 categories)
✓ Health check passed

Store is ready at http://localhost:8080
Admin API: http://localhost:8080/api/v1/admin
API Docs: http://localhost:8080/docs
```

---

## 6. Non-Goals (v0)

* No Kubernetes manifests (future)
* No CI/CD pipeline templates
* No Terraform / cloud provisioning
* No auto-scaling configuration
