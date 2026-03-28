# 🛠️ Deployment & Operations — Runbook (v0)

## 1. Prerequisites

---

* Go (for build) OR compiled binary
* Postgres database

---

---

## 2. Setup

---

### 2.1 Clone repository

```bash
git clone <repo>
cd app
```

---

---

### 2.2 Configure environment

---

Create `.env`:

```env
DB_URL=postgres://user:pass@localhost:5432/app
APP_ENV=prod
```

---

---

### 2.3 Configure system

---

Edit:

```plaintext
/configs/config.yaml
```

---

---

### 2.4 Run migrations

```bash
app migrate
```

---

---

## 3. Run Application

---

### Start API

```bash
app serve
```

---

---

### Start Worker

```bash
app worker
```

---

---

## 4. Verify System

---

### Health check

```bash
curl http://localhost:8080/health
```

---

---

### Logs

```bash
tail -f logs or stdout
```

---

---

## 5. Common Operations

---

### Reindex search

```bash
app search:reindex
```

---

---

### Export config

```bash
app config:export
```

---

---

### Import config

```bash
app config:import config.yaml
```

---

---

## 6. Deployment (Simple)

---

### Build binary

```bash
go build -o app ./cmd/api
```

---

### Run

```bash
./app serve
./app worker
```

---

---

## 7. Deployment (Docker — Optional)

---

```dockerfile
FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o app ./cmd/api

CMD ["./app", "serve"]
```

---

---

## 8. Scaling

---

### API scaling

* run multiple instances
* use load balancer

---

### Worker scaling

```bash
app worker
app worker
app worker
```

---

---

## 9. Backup

---

### Database

```bash
pg_dump > backup.sql
```

---

---

### Media

```bash
tar -czf media.tar.gz ./media
```

---

---

## 10. Troubleshooting

---

### App won’t start

* check `.env`
* check DB connection

---

### Jobs not processing

* ensure worker is running

---

### Payments not updating

* check webhook endpoint
* check logs

---

---

## 11. Production Checklist

---

* [ ] Postgres secured
* [ ] HTTPS enabled
* [ ] backups configured
* [ ] logs monitored
* [ ] worker running

---

---

## 12. Summary

To run system:

```bash
app migrate
app serve
app worker
```

---

> That’s it. Everything else is optional complexity.

---
