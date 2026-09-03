package scheduler

import "context"

// RegistrationEntry is one task's name and cron spec, as passed to
// Scheduler.Register.
type RegistrationEntry struct {
	Name string
	Spec string
}

// Store is what a live Scheduler needs from persistence to support
// cross-process admin introspection and control: recording its current
// registrations once at Start (so Catalog.List reflects reality regardless
// of which process actually called Register — see Catalog's doc comment),
// and checking a per-task enable override on every tick before firing.
// Optional: a Scheduler constructed without a Store behaves exactly as
// before this existed — always enabled, not introspectable from another
// process.
type Store interface {
	// UpsertRegistrations records the calling process's currently
	// registered tasks. Must not change an existing row's enabled state —
	// only Catalog.SetEnabled does that — so an admin override survives
	// across a process restart re-registering the same tasks.
	UpsertRegistrations(ctx context.Context, entries []RegistrationEntry) error

	// IsEnabledBatch reports the enabled state for each of the given names
	// in one round-trip — the tick loop may need to check several matching
	// tasks in the same minute, and a per-task round-trip would multiply
	// tick latency by the number of matches. A name absent from the
	// returned map is treated as enabled (default-on), matching Store's
	// role as a pure override check, not a registration gate.
	IsEnabledBatch(ctx context.Context, names []string) (map[string]bool, error)
}

// LocalTrigger is implemented by a live, in-process Scheduler — invokes a
// registered task's fn directly and synchronously, bypassing Postgres
// entirely, returning only once the task's fn has actually run. Only
// meaningful from the same process that called Register for that task;
// Catalog.Trigger uses this when available and returns a clear conflict
// error when it isn't (this process has no embedded scheduler).
//
// A non-nil error means the task's fn panicked — that's the only failure
// mode this can detect. fn is a bare func() with no error return, so a
// task that logs-and-swallows its own internal failure (e.g. a failed
// enqueue) still reports as triggered successfully; callers relying on
// this to mean "the underlying work was actually produced," not just
// "didn't panic," should know that distinction doesn't hold today.
type LocalTrigger interface {
	TriggerLocal(name string) error
}
