# ---- Build stage ----
# Digest-pinned Go 1.25.13+ alpine — keep in lockstep with go.mod (Dependabot refreshes digests).
# Do NOT set GOTOOLCHAIN=auto: builds must use only the toolchain in this image.
FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS builder

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a statically linked binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /shopanda ./cmd/api

# ---- Runtime stage ----
# Prefer Dependabot docker PRs to refresh digests.
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

# wget is needed for the HEALTHCHECK; ca-certificates for any HTTPS calls.
RUN apk add --no-cache ca-certificates wget \
    && adduser -D -u 1000 appuser

# Application binary.
COPY --from=builder /shopanda /usr/local/bin/shopanda

# Migrations, config template, OpenAPI spec, and default theme.
COPY migrations/            /app/migrations/
COPY configs/config.example.yaml /app/configs/config.yaml
COPY openapi.yaml           /app/openapi.yaml
COPY themes/                /app/themes/

WORKDIR /app

USER appuser

EXPOSE 8080

# Liveness only: process is up. Do not point HEALTHCHECK at /readyz — a DB outage
# would restart/kill the container instead of draining traffic. Use /readyz for
# orchestrator readiness / load-balancer traffic control.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["shopanda"]
CMD ["serve"]
