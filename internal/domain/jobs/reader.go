package jobs

import (
	"context"
	"time"
)

// ListFilter selects jobs for admin listing. Type and Status are optional
// (empty = no filter on that dimension). Status is the typed Status, not a
// bare string — this keeps it distinct from Type and from any other
// string-based type a caller might otherwise mix up, and documents that
// the field holds one of the Status constants. It does not validate enum
// membership: Go freely assigns an untyped string constant (including a
// typo'd one) to a Status field, and an explicit conversion
// (Status("bogus")) compiles too — either still reaches the query and
// simply matches zero rows, the same as today.
type ListFilter struct {
	Type   string
	Status Status
	Limit  int
	Offset int
}

// Summary is a lightweight read model for listing jobs — omits Payload,
// which can be arbitrarily large JSON, since a list view has no use for it.
type Summary struct {
	ID         string
	Type       string
	Status     Status
	Attempts   int
	MaxRetries int
	RunAt      time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Detail is the full read model for a single job.
type Detail struct {
	Summary
	Payload map[string]interface{}
	// LastError is the most recently recorded failure message, or empty if
	// the job has never failed. Only the most recent message is kept —
	// there is no per-attempt failure history.
	LastError string
}

// Reader is a read-only port over job state, separate from Queue (which is
// write-oriented: enqueue/dequeue/complete/fail). Keeping introspection off
// Queue matters because a broker-backed Queue implementation (Redis,
// RabbitMQ, Kafka, SQS) has no queryable job table the way the Postgres
// implementation does — folding List/Get/CountsByStatus into Queue would
// force every future Queue implementation to either fake support for
// methods it structurally can't provide, or leave them unimplemented
// against an interface that claims to support them. Job introspection is
// Postgres-queue-only for now; see PR-1028's own follow-up note for what a
// broker-backed equivalent would need (e.g. a shadow status table the
// worker writes to regardless of which Queue backend is active).
type Reader interface {
	// List returns a page of jobs matching filter, most recently created
	// first.
	List(ctx context.Context, filter ListFilter) ([]Summary, error)

	// Get returns a single job's full detail. Returns (nil, nil) when no
	// job with that ID exists.
	Get(ctx context.Context, id string) (*Detail, error)

	// CountsByStatus returns the number of jobs currently in each status.
	// A status with zero jobs is simply absent from the map, not present
	// with a zero value.
	CountsByStatus(ctx context.Context) (map[Status]int, error)
}
