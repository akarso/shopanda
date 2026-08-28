package jobs

import (
	"context"
	"fmt"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
)

// maxListLimit bounds List's page size — same ceiling convention used by
// admin-facing list endpoints elsewhere in this codebase (e.g. product,
// audit log listings).
const maxListLimit = 100

// defaultListLimit is applied when the caller doesn't specify one
// (filter.Limit <= 0) — keeps a naive "list everything" call from
// accidentally scanning the whole jobs table.
const defaultListLimit = 20

// maxListOffset bounds how deep a single List call can page — an
// unbounded offset forces Postgres to scan and discard every row before
// it, which gets more expensive as the jobs table grows, for a request
// that returns nothing useful past a reasonable depth anyway. A stopgap
// for this PR (no HTTP surface yet, so nothing external can actually
// supply an arbitrary offset today) — real deep-pagination usage should
// move to keyset pagination (a created_at/id cursor) rather than raising
// this further; see PR-1029's admin API, the first place an untrusted
// caller can set Offset at all.
const maxListOffset = 100_000

// Service is the application service over job state: read-only
// introspection (List/Get/CountsByStatus, PR-1028) plus admin-triggered
// lifecycle corrections (Retry/Cancel, PR-1029). It exists to apply
// pagination policy (default/max page size) once, centrally, rather than
// trusting every future domainjobs.Reader implementation to enforce it
// consistently — the reader itself trusts filter.Limit/Offset as given.
type Service struct {
	reader domainjobs.Reader
	admin  domainjobs.Admin
}

// NewService creates a Service backed by reader and admin.
func NewService(reader domainjobs.Reader, admin domainjobs.Admin) (*Service, error) {
	if reader == nil {
		return nil, fmt.Errorf("jobs.NewService: nil reader")
	}
	if admin == nil {
		return nil, fmt.Errorf("jobs.NewService: nil admin")
	}
	return &Service{reader: reader, admin: admin}, nil
}

// List returns a page of jobs matching filter, most recently created
// first. filter.Limit is defaulted (if <= 0) and capped (if too large);
// filter.Offset is floored at 0 and capped at maxListOffset.
func (s *Service) List(ctx context.Context, filter domainjobs.ListFilter) ([]domainjobs.Summary, error) {
	switch {
	case filter.Limit <= 0:
		filter.Limit = defaultListLimit
	case filter.Limit > maxListLimit:
		filter.Limit = maxListLimit
	}
	switch {
	case filter.Offset < 0:
		filter.Offset = 0
	case filter.Offset > maxListOffset:
		filter.Offset = maxListOffset
	}
	return s.reader.List(ctx, filter)
}

// Get returns a single job's full detail, or (nil, nil) if no job with
// that ID exists.
func (s *Service) Get(ctx context.Context, id string) (*domainjobs.Detail, error) {
	if id == "" {
		return nil, fmt.Errorf("jobs: get: empty id")
	}
	return s.reader.Get(ctx, id)
}

// CountsByStatus returns the number of jobs currently in each status.
func (s *Service) CountsByStatus(ctx context.Context) (map[domainjobs.Status]int, error) {
	return s.reader.CountsByStatus(ctx)
}

// Retry resets a failed job back to pending. See domainjobs.Admin for the
// full error contract (not-found vs. wrong-status).
func (s *Service) Retry(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("jobs: retry: empty id")
	}
	return s.admin.Retry(ctx, id)
}

// Cancel marks a pending job cancelled. See domainjobs.Admin for the full
// error contract, including why a processing job cannot be cancelled.
func (s *Service) Cancel(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("jobs: cancel: empty id")
	}
	return s.admin.Cancel(ctx, id)
}
