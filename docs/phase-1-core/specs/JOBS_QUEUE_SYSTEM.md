# ⚙️ Background Jobs & Queue System — v0 Specification

## 1. Overview

Provides:

* async job execution
* retry handling
* decoupling of heavy tasks

Design goals:

* simple default (in-process)
* pluggable backends (Redis, RabbitMQ, etc.)
* idempotent execution
* minimal complexity

---

## 2. Core Concepts

---

### 2.1 Job

```go
type Job struct {
    ID        string

    Type      string
    Payload   map[string]interface{}

    Attempts  int
    MaxRetries int

    CreatedAt time.Time
}
```

---

---

### 2.2 Job Handler

```go
type JobHandler interface {
    Name() string
    Handle(job Job) error
}
```

---

---

## 3. Queue Interface

---

```go
type Queue interface {
    Enqueue(job Job) error
    Dequeue() (Job, error)
}
```

---

---

## 4. Default Implementation (Core)

---

### In-Memory Queue

* simple
* single process
* good for dev/small setups

---

---

## 5. Worker

---

```go
func StartWorker(q Queue) {
    for {
        job := q.Dequeue()

        handler := ResolveHandler(job.Type)

        err := handler.Handle(job)

        if err != nil {
            Retry(job)
        }
    }
}
```

---

---

## 6. Retry Strategy

---

* exponential backoff (future)
* max retries enforced

---

---

## 7. Job Dispatching

---

```go
Dispatch(Job{
    Type: "send_email",
    Payload: {...},
})
```

---

---

## 8. Event Integration

---

Events trigger jobs:

```go
On("order.created", func(e Event) {
    Dispatch(Job{Type: "send_order_email"})
})
```

---

---

## 9. Pluggable Backends

---

Plugins can implement:

* Redis queue
* RabbitMQ
* SQS

---

```go
RegisterQueue("redis", RedisQueue{})
```

---

---

## 10. Idempotency

---

Handlers MUST:

* be safe to run multiple times
* check state before acting

---

---

## 11. Non-Goals (v0)

---

* no job scheduling (cron)
* no priority queues
* no distributed guarantees

---

---

## 12. Summary

Queue system v0 provides:

> a minimal async execution layer that decouples heavy operations from request lifecycle.

---


# ⚙️ Background Jobs & Queue System — v0.1 (Postgres-Backed)

## 1. Overview (Update)

Core now provides:

* in-memory queue (dev)
* Postgres-backed queue (production-ready)

---

## 2. Postgres Queue Implementation

---

### 2.1 Jobs Table

```sql
CREATE TABLE jobs (
  id UUID PRIMARY KEY,

  type TEXT NOT NULL,
  payload JSONB NOT NULL,

  attempts INT NOT NULL DEFAULT 0,
  max_retries INT NOT NULL DEFAULT 3,

  status TEXT NOT NULL DEFAULT 'pending',

  run_at TIMESTAMP NOT NULL DEFAULT NOW(),

  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

### Status values:

```text
pending → processing → done
        ↘ failed
```

---

---

## 3. Enqueue

---

```sql
INSERT INTO jobs (id, type, payload)
VALUES ($1, $2, $3);
```

---

---

## 4. Dequeue (CRITICAL)

---

```sql
SELECT *
FROM jobs
WHERE status = 'pending'
  AND run_at <= NOW()
ORDER BY created_at
FOR UPDATE SKIP LOCKED
LIMIT 1;
```

---

Then:

```sql
UPDATE jobs
SET status = 'processing',
    attempts = attempts + 1
WHERE id = $1;
```

---

---

## 5. Worker Flow

---

```text
1. begin transaction
2. select job (skip locked)
3. mark as processing
4. commit
5. execute handler
6. update status
```

---

---

## 6. Completion

---

### Success:

```sql
UPDATE jobs
SET status = 'done'
WHERE id = $1;
```

---

### Failure:

```sql
UPDATE jobs
SET status = 'pending', run_at = NOW() + interval '10 seconds'
WHERE id = $1;
```

---

### Max retries:

```sql
UPDATE jobs
SET status = 'failed'
WHERE attempts >= max_retries;
```

---

---

## 7. Retry Strategy

---

v0:

* fixed delay (e.g. 10s)

future:

* exponential backoff

---

---

## 8. Idempotency

---

Handlers MUST:

* check state before execution
* tolerate duplicate execution

---

---

## 9. Concurrency

---

* multiple workers safe
* no duplicate job processing
* horizontal scaling supported

---

---

## 10. Configuration

---

```yaml
queue:
  driver: postgres
```

---

---

## 11. Observability (Minimal)

---

Future fields:

```sql
error TEXT
```

---

---

## 12. Cleanup

---

Optional:

```sql
DELETE FROM jobs WHERE status = 'done' AND created_at < NOW() - interval '7 days';
```

---

---

## 13. Extensibility

---

Plugins can:

* replace queue
* add scheduling
* add priorities

---

---

## 14. Summary

Postgres queue provides:

> a production-ready, zero-dependency job system leveraging database capabilities.

It ensures:

* persistence
* scalability
* simplicity

---

## For later consideration:

1. Visibility timeout (advanced)

If worker crashes:
→ job returns to queue automatically

2. Job deduplication

Prevent:

duplicate email sends
duplicate webhooks

3. Job types separation
email.*
search.*
payment.*