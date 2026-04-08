package scheduler

import "context"

// Scheduler provides periodic job scheduling.
// Implementations accept cron-style specs and fire registered functions on schedule.
type Scheduler interface {
	// Register adds a named task with a cron spec.
	// The spec uses standard 5-field cron syntax: minute hour day-of-month month day-of-week.
	// Must be called before Start.
	Register(name string, spec string, fn func())

	// Start begins evaluating schedules. Blocks until the context is cancelled
	// or Stop is called.
	Start(ctx context.Context)

	// Stop signals the scheduler to shut down. Safe to call multiple times.
	Stop()
}
