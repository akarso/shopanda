package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Compile-time checks.
var (
	_ domainscheduler.Store   = (*SchedulerStore)(nil)
	_ domainscheduler.Catalog = (*SchedulerStore)(nil)
)

// SchedulerStore is the Postgres-backed implementation of both
// domainscheduler.Store (consumed by a live *cron.Scheduler for
// registration/override persistence) and domainscheduler.Catalog (consumed
// by the admin HTTP layer). One table, one type, two roles — see PR-1030's
// doc comments on those interfaces for why this has to be Postgres-backed
// rather than living in any single process's memory.
//
// Trigger additionally needs a live registered task's fn to actually call,
// which only exists in a process that embeds a running Scheduler — set via
// SetLocalTrigger from that process's own wiring after constructing it.
// local stays nil in a process with no embedded scheduler (production
// `serve` by default), and Trigger returns a clear conflict error there
// instead of silently no-op'ing.
type SchedulerStore struct {
	db    *sql.DB
	local domainscheduler.LocalTrigger
}

// NewSchedulerStore creates a SchedulerStore.
func NewSchedulerStore(db *sql.DB) (*SchedulerStore, error) {
	if db == nil {
		return nil, fmt.Errorf("postgres: scheduler store: nil db")
	}
	return &SchedulerStore{db: db}, nil
}

// SetLocalTrigger attaches a live in-process Scheduler for Trigger to use.
// Optional — call it only from a process that actually embeds one.
func (s *SchedulerStore) SetLocalTrigger(local domainscheduler.LocalTrigger) {
	s.local = local
}

// UpsertRegistrations records the calling process's currently registered
// tasks. Deliberately does not touch `enabled` for a task that already has
// a row — only SetEnabled does that — so an admin override survives a
// process restart re-registering the same tasks.
func (s *SchedulerStore) UpsertRegistrations(ctx context.Context, entries []domainscheduler.RegistrationEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: scheduler store: upsert registrations: begin: %w", err)
	}
	defer tx.Rollback()

	const q = `INSERT INTO scheduler_tasks (name, spec, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (name) DO UPDATE SET spec = EXCLUDED.spec, updated_at = NOW()`
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, q, e.Name, e.Spec); err != nil {
			return fmt.Errorf("postgres: scheduler store: upsert registrations: %w", err)
		}
	}
	return tx.Commit()
}

// IsEnabledBatch reports the enabled state for each of the given names in
// one query. A name absent from the returned map (no stored row) is
// treated as enabled by the caller (default-on) — matches a task that has
// never been touched through the admin API, or that hasn't upserted
// itself yet on this process's very first tick.
func (s *SchedulerStore) IsEnabledBatch(ctx context.Context, names []string) (map[string]bool, error) {
	out := make(map[string]bool, len(names))
	if len(names) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name, enabled FROM scheduler_tasks WHERE name = ANY($1)`, names)
	if err != nil {
		return nil, fmt.Errorf("postgres: scheduler store: is enabled batch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, fmt.Errorf("postgres: scheduler store: is enabled batch scan: %w", err)
		}
		out[name] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: scheduler store: is enabled batch rows: %w", err)
	}
	return out, nil
}

// List returns every known task's current catalog entry, ordered by name.
func (s *SchedulerStore) List(ctx context.Context) ([]domainscheduler.CatalogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, spec, enabled FROM scheduler_tasks ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("postgres: scheduler store: list: %w", err)
	}
	defer rows.Close()

	out := make([]domainscheduler.CatalogEntry, 0)
	for rows.Next() {
		var name, spec string
		var enabled bool
		if err := rows.Scan(&name, &spec, &enabled); err != nil {
			return nil, fmt.Errorf("postgres: scheduler store: list scan: %w", err)
		}
		// A stored spec is only ever written by Scheduler.Register, which
		// validates it first — parse failure here would mean stored data
		// diverged from what's actually running, which shouldn't happen.
		// Surface a zero NextRun rather than failing the whole list.
		nextRun, err := cron.NextRun(spec, time.Now())
		if err != nil {
			nextRun = time.Time{}
		}
		out = append(out, domainscheduler.CatalogEntry{
			Name:    name,
			Spec:    spec,
			NextRun: nextRun,
			Enabled: enabled,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: scheduler store: list rows: %w", err)
	}
	return out, nil
}

// SetEnabled persists an enable/disable override for name. Returns a
// *apperror.Error with CodeNotFound if no task with that name has ever
// registered.
func (s *SchedulerStore) SetEnabled(ctx context.Context, name string, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE scheduler_tasks SET enabled = $2, updated_at = NOW() WHERE name = $1`, name, enabled)
	if err != nil {
		return fmt.Errorf("postgres: scheduler store: set enabled: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres: scheduler store: set enabled rows: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound(fmt.Sprintf("no scheduled task named %q", name))
	}
	return nil
}

// Trigger invokes a registered task's fn immediately via the attached
// local Scheduler (see SetLocalTrigger). Returns a *apperror.Error with
// CodeNotFound if no task with that name has ever registered anywhere, or
// CodeConflict if this process has no live scheduler embedded to invoke it
// from, or if the task is known to the shared registry but isn't among
// this process's own local registrations (see the divergent-registrations
// note below).
func (s *SchedulerStore) Trigger(ctx context.Context, name string) error {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT true FROM scheduler_tasks WHERE name = $1`, name).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.NotFound(fmt.Sprintf("no scheduled task named %q", name))
	}
	if err != nil {
		return fmt.Errorf("postgres: scheduler store: trigger lookup: %w", err)
	}
	if s.local == nil {
		return apperror.Conflict("this server process has no embedded scheduler to trigger from — run with the scheduler embedded (dev mode) or use the standalone scheduler process")
	}

	err = s.local.TriggerLocal(name)
	var appErr *apperror.Error
	if errors.As(err, &appErr) && appErr.Code == apperror.CodeNotFound {
		// The shared registry confirmed name exists (checked above), but
		// this process's own in-memory registrations don't include it —
		// only possible if more than one process embeds a scheduler with
		// a divergent task set (e.g. plugin-driven cron triggers differ
		// between two `serve --embed-scheduler` instances). Running more
		// than one embedded scheduler is already discouraged (it
		// double-fires every tick — see RUNTIME_MODES.md), so this is an
		// unsupported-configuration signal, not a genuine "no such task."
		// Surface it as a conflict naming the real cause instead of
		// letting TriggerLocal's own not-found (correct from its narrower
		// point of view) read as "this task doesn't exist anywhere."
		return apperror.Conflict(fmt.Sprintf("task %q is registered but not part of this process's local scheduler — its embedded task set differs from whichever process registered it (running more than one embedded scheduler with different task sets is unsupported)", name))
	}
	return err
}
