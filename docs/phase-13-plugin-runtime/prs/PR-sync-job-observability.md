# PR — Sync job observability (request_id, Attempts, JobResult, CountsByType)

## Intent

Every product plugin needs HTTP→job correlation, attempt/duration visibility, and
queue depth by type. That belongs in the platform so merchants do not reimplement
it per store.

## Changes

- `plugin.InjectRequestID` + automatic injection on `Enqueue` / sync enqueue paths
- `extapi.SyncJobContext.Attempts`
- Worker logs: `request_id`, `duration_ms`; metrics: `JobResult` →
  `shopanda_job_results_total` / `shopanda_job_duration_seconds`
- `jobs.Reader.CountsByType` (+ Postgres + `application/jobs.Service`)
- Guide: `docs/guides/SYNC_JOB_OBSERVABILITY.md`

## Out of scope

- Changing `/healthz` / `/readyz`
- Free-form job types
- Product-specific `correlation_id` naming (alias at the product plugin)
