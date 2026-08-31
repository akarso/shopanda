package scheduler

import (
	"context"
	"fmt"

	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
)

// Service is the application service over the scheduler's registered task
// catalog — admin-triggered introspection and corrections (list/trigger/
// enable/disable) on top of the domain Catalog port. Exists to centralize
// name validation once, matching jobs.Service's role for job admin
// (PR-1029).
type Service struct {
	catalog domainscheduler.Catalog
}

// NewService creates a Service backed by catalog.
func NewService(catalog domainscheduler.Catalog) (*Service, error) {
	if catalog == nil {
		return nil, fmt.Errorf("scheduler.NewService: nil catalog")
	}
	return &Service{catalog: catalog}, nil
}

// List returns every known registered task's current catalog entry.
func (s *Service) List(ctx context.Context) ([]domainscheduler.CatalogEntry, error) {
	return s.catalog.List(ctx)
}

// Trigger fires a registered task immediately, out-of-band from its normal
// tick, regardless of a disabled override. See domainscheduler.Catalog for
// the full error contract (not-found vs. no-local-scheduler conflict).
func (s *Service) Trigger(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("scheduler: trigger: empty name")
	}
	return s.catalog.Trigger(ctx, name)
}

// SetEnabled persists an enable/disable override for a registered task.
func (s *Service) SetEnabled(ctx context.Context, name string, enabled bool) error {
	if name == "" {
		return fmt.Errorf("scheduler: set enabled: empty name")
	}
	return s.catalog.SetEnabled(ctx, name, enabled)
}
