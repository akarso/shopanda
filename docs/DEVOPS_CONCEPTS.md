# 🚀 Deployment & Operations — Concepts (v0)

## 1. Overview

This system is designed to:

* run as a **single binary**
* require **minimal infrastructure**
* scale by adding components incrementally

Core philosophy:

> Start simple → scale only when needed

---

## 2. Deployment Model

---

### Single Node (Default)

```text
app (API + workers)
↓
Postgres
↓
(optional) filesystem
```

---

### Components

* API server (HTTP)
* Worker (jobs)
* Postgres (required)
* Filesystem (media)

---

---

## 3. Process Types

---

### API

* handles HTTP requests
* stateless

---

### Worker

* processes jobs (emails, payments, etc.)
* can scale horizontally

---

---

## 4. Configuration

---

### Sources:

* `.env` → secrets
* `config.yaml` → system config
* DB → runtime config

---

---

## 5. Storage

---

### Required:

* Postgres

---

### Optional:

* local filesystem (default)
* S3/CDN (plugin)

---

---

## 6. Queue

---

### Default:

* Postgres-backed queue

---

### Optional:

* Redis / RabbitMQ (plugins)

---

---

## 7. Scaling Strategy

---

### Step 1: Vertical

* increase CPU/RAM

---

### Step 2: Horizontal API

* multiple API instances
* load balancer

---

### Step 3: Workers

* multiple workers
* parallel job processing

---

### Step 4: External services (optional)

* CDN
* search engine
* queue system

---

---

## 8. Observability

---

### Core:

* structured JSON logs (stdout)

---

### Optional:

* log aggregation (Grafana, etc.)

---

---

## 9. Deployment Targets

---

Supports:

* local machine
* VPS
* Docker (optional)
* FreeBSD jails (future)
* cloud platforms

---

---

## 10. Philosophy

---

* no required containers
* no required microservices
* no required external services

---

> Everything works with: **binary + Postgres**

---

## 11. Summary

System is:

* self-contained
* horizontally scalable
* incrementally extensible

---
