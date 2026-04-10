package seed

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/logger"
)

// Seeder defines a unit of seed data that can be applied to the system.
// Each seeder must be idempotent — safe to run multiple times.
type Seeder interface {
	// Name returns a unique human-readable identifier (e.g. "admin-user").
	Name() string

	// Seed applies the seed data. Implementations must check for existing
	// data before inserting to ensure idempotency.
	Seed(ctx context.Context, deps Deps) error
}

// Deps provides the dependencies available to seeders.
type Deps struct {
	DB     *sql.DB
	Logger logger.Logger
}

// Result holds the summary of a seed run.
type Result struct {
	Executed int
	Skipped  int
}

// Registry holds an ordered list of seeders.
type Registry struct {
	seeders []Seeder
	names   map[string]bool
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{names: make(map[string]bool)}
}

// Register adds a seeder to the registry. Panics on duplicate names.
func (r *Registry) Register(s Seeder) {
	name := s.Name()
	if name == "" {
		panic("seed: seeder name must not be empty")
	}
	if r.names[name] {
		panic(fmt.Sprintf("seed: duplicate seeder name %q", name))
	}
	r.names[name] = true
	r.seeders = append(r.seeders, s)
}

// Run executes all registered seeders in order.
// A failing seeder stops execution and returns the error.
func (r *Registry) Run(ctx context.Context, deps Deps) (*Result, error) {
	result := &Result{}
	for _, s := range r.seeders {
		deps.Logger.Info("seed.run", map[string]interface{}{
			"seeder": s.Name(),
		})
		if err := s.Seed(ctx, deps); err != nil {
			return result, fmt.Errorf("seed %q: %w", s.Name(), err)
		}
		result.Executed++
	}
	return result, nil
}

// SeederFunc adapts a plain function into a Seeder.
type SeederFunc struct {
	name string
	fn   func(ctx context.Context, deps Deps) error
}

// NewSeederFunc creates a SeederFunc with the given name and function.
func NewSeederFunc(name string, fn func(ctx context.Context, deps Deps) error) *SeederFunc {
	if fn == nil {
		panic("seed: NewSeederFunc: fn must not be nil")
	}
	return &SeederFunc{name: name, fn: fn}
}

func (s *SeederFunc) Name() string                              { return s.name }
func (s *SeederFunc) Seed(ctx context.Context, deps Deps) error { return s.fn(ctx, deps) }
