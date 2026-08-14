# ---- Build stage ----
# Digest-pinned Go 1.25.13+ alpine — keep in lockstep with go.mod (Dependabot refreshes digests).
# Do NOT set GOTOOLCHAIN=auto: builds must use only the toolchain in this image.
FROM golang:1.25.13-alpine@sha256:844b27705f54e73773e0f9bc3c780633b9d7f4b4831bf35cdad02a81a4c80bd0 AS builder

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a statically linked binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /shopanda ./cmd/api

# ---- Runtime stage ----
# Prefer Dependabot docker PRs to refresh digests.
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

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
