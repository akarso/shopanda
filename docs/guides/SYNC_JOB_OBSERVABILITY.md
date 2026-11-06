# Sync job observability

How plugins get request correlation, job duration metrics, and queue depth
**without** inventing product-specific plumbing. Prefer these platform APIs over
per-plugin log wrappers or custom SQL.

## Request correlation (`request_id`)

HTTP middleware already sets `request_id` from `X-Request-ID` (or generates one)
on the request context (`internal/platform/requestctx`).

When a plugin calls `Integration.Enqueue(ctx, name, payload)` (or the event/cron
enqueue path that receives a context), Shopanda:

1. Copies `requestctx.RequestID(ctx)` into the **inner** job payload as
   `request_id` when the key is not already set.
2. Logs `integration.sync_job.enqueued` with `request_id` when present.
3. On the worker, logs `worker.job.processing` / `failed` / `done` with
   `request_id` read from the envelope payload, plus `attempt` and
   `duration_ms` where applicable.

Handlers receive the same value on `SyncJobContext.Payload["request_id"]` and
`SyncJobContext.Attempts` (queue attempt counter at dequeue).

```go
// HTTP handler — pass the request context; do not invent a second ID.
_ = sdk.Integration("acme").Enqueue(r.Context(), "webhook.ingest", map[string]interface{}{
    "order_id": orderID,
})

// Sync handler — log with platform fields.
func handle(_ context.Context, job extapi.SyncJobContext) error {
    fields := map[string]interface{}{
        "job_id":  job.JobID,
        "attempt": job.Attempts,
    }
    if rid, _ := job.Payload["request_id"].(string); rid != "" {
        fields["request_id"] = rid
    }
    job.Logger.Info("acme.webhook.ingest", fields)
    return nil
}
```

Product sites that prefer the name `correlation_id` in their own log lines may
alias `request_id` — the platform field remains `request_id`.

## Metrics

The worker records:

| Metric | Labels | Meaning |
| --- | --- | --- |
| `shopanda_job_failures_total` | `job_type` | Failures (compat; still incremented on handler/unknown-type failure) |
| `shopanda_job_results_total` | `job_type`, `outcome` | Finished attempts (`success` / `failed`) |
| `shopanda_job_duration_seconds` | `job_type`, `outcome` | Handler duration histogram |

`job_type` is always a registered type (e.g. `integration.sync.acme.webhook.ingest`)
or the sentinel `unknown`. Never put job IDs or payload values in labels.

## Queue depth by type

Postgres queue only (`jobs.Reader`):

```go
counts, err := reader.CountsByType(ctx, "integration.sync.acme.webhook.ingest")
// map[jobs.Status]int — pending / processing / failed / …; zero statuses omitted
```

Also available via `application/jobs.Service.CountsByType`. Use this for plugin
health/admin surfaces that report depth for **their** job types. Do not page
`List` just to approximate depth.

Brokers that do not implement `jobs.Reader` have no depth API (same constraint
as `jobs:list`).

## What plugins should not do

- Re-implement search_path / correlation injection in every product plugin.
- Extend `/healthz` or `/readyz` with queue depth (those stay process/DB probes).
- Register free-form job types outside `integration.sync.{slug}.{name}`.
