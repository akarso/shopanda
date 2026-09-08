-- Tracks a search reindex run (PR-1033): moving `search:reindex` onto the
-- job queue means the scan-and-index work now happens inside a worker
-- process, not the CLI process the operator is watching — this row is
-- what lets the CLI (--wait) and, later, an admin progress endpoint
-- (PR-1035) observe a run that's happening somewhere else.
--
-- scope/scope_params carry the full-scope-only "all" scope in this PR;
-- PR-1034 adds partial/scoped values without a schema change here.
-- processed_count is updated incrementally by the worker as it goes (not
-- just at the end), which is what makes a progress readout meaningful.
CREATE TABLE IF NOT EXISTS search_index_runs (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    scope_params    JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL CHECK (status IN ('processing', 'completed', 'failed')),
    total_count     INT NOT NULL DEFAULT 0,
    processed_count INT NOT NULL DEFAULT 0,
    error_count     INT NOT NULL DEFAULT 0,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    last_error      TEXT
);
