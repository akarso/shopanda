# Sync job observability

How plugins get request correlation, job duration metrics, and queue depth
**without** inventing product-specific plumbing. Prefer these platform APIs over
per-plugin log wrappers or custom SQL.

## Request correlation (`request_id`)

HTTP middleware sets `request_id` from `X-Request-ID` (or generates one) on the
request context (`internal/platform/requestctx`). Inbound headers are capped at
128 characters and restricted to a safe charset (letters, digits, `-_.:/`);
oversized or unsafe values are replaced with a generated ID before they can
reach job payloads or logs.

### Manual enqueue

When a plugin calls `Integration.Enqueue(ctx, name, payload)` with the HTTP
request context, Shopanda:

1. Copies `requestctx.RequestID(ctx)` into the **inner** job payload as
   `request_id` when the key is missing, empty, nil, or non-string.
2. Logs `integration.sync_job.enqueued` with `request_id` when present.
3. On the worker, logs `worker.job.processing` / `failed` / `done` /
   `complete_failed` with `request_id` (and `duration_ms` where applicable).

Handlers receive the same value on `SyncJobContext.Payload["request_id"]` and
`SyncJobContext.Attempts` (queue attempt counter at dequeue). The sync worker
handler also reinjects that payload `request_id` onto the handler context via
`requestctx.WithRequestID` when the worker ctx does not already carry one, so
`requestctx.RequestID(ctx)` inside a sync handler matches the enqueue path.

```go
// HTTP handler — pass the request context; do not invent a second ID.
_ = sdk.Integration("acme").Enqueue(r.Context(), "webhook.ingest", map[string]interface{}{
    "order_id": orderID,
})

// Idempotent one-job-per-entity (UUID job id). JobsPort must be bound.
// On Conflict, Enqueue itself: same Type + pending/processing → nil (no-op);
// terminal → Requeue(WithRequeueTerminal); wrong Type → error.
// Second call's payload is discarded — use a new id to change inputs.
if err := sdk.Integration("acme").Enqueue(r.Context(), "gen.run", payload, pluginsdk.WithJobID(entityUUID)); err != nil {
    return err
}
```

### Event path (`RegisterOnEvent`)

Idempotent job id (optional): `RegisterOnEvent(…, pluginsdk.WithEventJobID(func(payload) uuid))`.
The callback sees a flattened public map (`order_id` at the top level for
`order.created`) **before** `request_id` is injected. Job ids are global per
queue — derive from job purpose + entity (e.g. UUID v5 over job type +
`order_id`), not a raw order UUID shared by multiple listeners. Conflict
resolves like `WithJobID` when `JobsPort` is bound.

`event.Bus.OnAsync` handlers intentionally receive the **shutdown context**, not
the Publish request context (so request cancel does not abort side effects).
Correlation still works:

1. `Publish` and `PublishAsync` stamp `Meta["request_id"]` from their context
   when Meta does not already set one (`PublishAsync` takes `ctx` for this —
   Publish's local stamped copy is not visible to fallback callers).
2. Async dispatch copies that Meta value onto the handler context.
3. `RegisterSyncJobEventTriggers` also copies Meta into the inner sync payload
   before `InjectRequestID` / `NewSyncJobEnvelope`.

So HTTP → `Publish(ctx, …)` (or Publish-fail → `PublishAsync(ctx, …)`) →
OnAsync enqueue yields `request_id` on the queued envelope without plugins
reading the Publish context directly.

Cron triggers use a timeout/`Background` context and typically have no
`request_id` (expected).

### Worker log fields

For sync envelopes the worker reads `payload.request_id`. For other job types
that store a top-level `request_id` (e.g. `webhook.deliver` after bus Meta
propagation), that top-level key is used. Prefer platform worker lines over
re-extracting `request_id` in every plugin handler unless you need extra
product fields.

Product sites that prefer the name `correlation_id` in their own log lines may
alias `request_id` — the platform field remains `request_id`.

## Metrics

The worker records:

| Metric | Labels | Meaning |
| --- | --- | --- |
| `shopanda_job_failures_total` | `job_type` | Failures (compat; still incremented on handler/unknown-type/Complete failure) |
| `shopanda_job_results_total` | `job_type`, `outcome` | Finished attempts (`success` / `failed`) |
| `shopanda_job_duration_seconds` | `job_type`, `outcome` | Handler duration histogram (buckets through 5m) |

`JobResult("success")` is recorded **only after** a successful `queue.Complete`.
If Complete fails after a successful handler, the attempt is
`outcome="failed"` (plus `JobFailure`) so success-rate dashboards stay honest
under redelivery. Unknown job types record `JobResult("unknown", "failed", 0)`.

`job_type` is always a registered type (e.g. `integration.sync.acme.webhook.ingest`)
or the sentinel `unknown`. Never put job IDs or payload values in labels.

## Queue depth by type

Postgres queue only (`jobs.Reader` / CapJobs `JobsPort`):

```go
// Prefer pluginsdk (Need CapJobs + Host.Jobs):
jp, ok := host.Jobs()
if !ok {
    return errJobsUnbound
}
counts, err := jp.CountsByType(ctx, "integration.sync.acme.webhook.ingest")
// map[pluginsdk.JobStatus]int — pending / processing / failed / …
// Empty/whitespace jobType errors at the port (before Reader). Zero statuses
// omitted by Postgres Reader; port forwards. CountsByType is unscoped (any type).

// application/jobs.Service.CountsByType also rejects empty/whitespace;
// jobs.Reader / Postgres reject empty jobType the same way.
```

Use this for plugin health/admin surfaces that report depth for **their** job
types. Do not page `List` just to approximate depth.

Brokers that do not implement `jobs.Reader` have no depth API (same constraint
as `jobs:list`). Adding `CountsByType` is a required method on `jobs.Reader`
(compile break for out-of-tree Reader implementations).

## What plugins should not do

- Re-implement search_path / correlation injection in every product plugin.
- Extend `/healthz` or `/readyz` with queue depth (those stay process/DB probes).
- Register free-form job types outside `integration.sync.{slug}.{name}`.
- Rely on OnAsync handler `ctx` being the Publish request context (it is not).
