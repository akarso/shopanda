# ---- Build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build a statically linked binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /shopanda ./cmd/api

# ---- Runtime stage ----
FROM alpine:3.21

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
