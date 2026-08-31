package scheduler

import (
	"context"
	"time"
)

// CatalogEntry describes one registered scheduled task's current state.
type CatalogEntry struct {
	Name    string
	Spec    string
	NextRun time.Time
	Enabled bool
}

// Catalog is a read+admin-write port over the scheduler's registered task
// state, backed by Postgres rather than any single process's in-memory
// Scheduler — the scheduler runs as a separate OS process from the API
// server in production (see docs/phase-4-refactoring/specs/RUNTIME_MODES.md:
// `serve` only embeds a scheduler in dev/opt-in mode; the standalone
// `scheduler` command is the norm in production), so introspection and
// enable/disable have to work regardless of which process is asking — the
// same reason job introspection (PR-1028) reads the jobs table instead of
// the in-process Worker. Kept separate from Scheduler (register/start/stop)
// for the same ISP reasoning as jobs.Reader/jobs.Admin vs. jobs.Queue: only
// one concrete implementation needs this, and Scheduler's existing callers
// (every Register* helper, and the stubScheduler test fake in
// internal/platform/runtime) have no reason to grow these methods.
type Catalog interface {
	// List returns every known registered task's current catalog entry,
	// ordered by name. "Known" means a task that has upserted itself via
	// Registrar at least once — a task that has never started in any
	// process can't appear here.
	List(ctx context.Context) ([]CatalogEntry, error)

	// Trigger invokes a registered task's fn immediately, out-of-band from
	// its normal tick, regardless of a disabled override — the override
	// gates the scheduler's own automatic tick, not an explicit manual
	// trigger. Returns a *apperror.Error with CodeNotFound if no task with
	// that name has ever registered, or CodeConflict if the task is known
	// but this process has no live scheduler embedded to actually invoke
	// it from (see LocalTrigger).
	Trigger(ctx context.Context, name string) error

	// SetEnabled persists an enable/disable override for a registered
	// task, checked by the scheduler's own tick-fire path before invoking
	// a task's fn (Trigger ignores it). Returns a *apperror.Error with
	// CodeNotFound if no task with that name has ever registered.
	SetEnabled(ctx context.Context, name string, enabled bool) error
}
