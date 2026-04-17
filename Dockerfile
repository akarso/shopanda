# ---- Build stage ----
# Pinned 2026-04-17; refresh periodically.
FROM golang:1.25-alpine@sha256:5caaf1cca9dc351e13deafbc3879fd4754801acba8653fa9540cea125d01a71f AS builder

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a statically linked binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /shopanda ./cmd/api

# ---- Runtime stage ----
# Pinned 2026-04-17; refresh periodically.
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

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["shopanda"]
CMD ["serve"]
