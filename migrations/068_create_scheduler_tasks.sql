-- Cross-process scheduler admin state (PR-1030): the cron scheduler runs as
-- a separate OS process from the API server in production (RUNTIME_MODES.md
-- — `serve` only embeds a scheduler in dev/opt-in mode), so admin
-- introspection and enable/disable overrides can't live in any one
-- process's memory. Whichever process actually registers tasks (the
-- standalone `scheduler` command, or `serve --embed-scheduler` in dev)
-- upserts its registrations here at Start; the same table is checked by
-- every running scheduler's tick loop before firing a task, so an override
-- set through the admin API (hosted by the API server) takes effect in the
-- real scheduler process without either process needing to know about the
-- other directly.
CREATE TABLE IF NOT EXISTS scheduler_tasks (
    name TEXT PRIMARY KEY,
    spec TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
