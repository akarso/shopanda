package seed_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/seed"
)

// --- test helpers ---

type stubLogger struct{}

func (stubLogger) Info(_ string, _ map[string]interface{})           {}
func (stubLogger) Warn(_ string, _ map[string]interface{})           {}
func (stubLogger) Error(_ string, _ error, _ map[string]interface{}) {}

type recordingSeeder struct {
	name    string
	called  bool
	seedErr error
}

func (s *recordingSeeder) Name() string { return s.name }
func (s *recordingSeeder) Seed(_ context.Context, _ seed.Deps) error {
	s.called = true
	return s.seedErr
}

// --- tests ---

func TestRegistry_RunEmpty(t *testing.T) {
	reg := seed.NewRegistry()
	result, err := reg.Run(context.Background(), seed.Deps{Logger: stubLogger{}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Executed != 0 {
		t.Errorf("Executed = %d, want 0", result.Executed)
	}
}

func TestRegistry_RunOrder(t *testing.T) {
	reg := seed.NewRegistry()
	var order []string

	reg.Register(seed.NewSeederFunc("first", func(_ context.Context, _ seed.Deps) error {
		order = append(order, "first")
		return nil
	}))
	reg.Register(seed.NewSeederFunc("second", func(_ context.Context, _ seed.Deps) error {
		order = append(order, "second")
		return nil
	}))
	reg.Register(seed.NewSeederFunc("third", func(_ context.Context, _ seed.Deps) error {
		order = append(order, "third")
		return nil
	}))

	result, err := reg.Run(context.Background(), seed.Deps{Logger: stubLogger{}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Executed != 3 {
		t.Errorf("Executed = %d, want 3", result.Executed)
	}
	if len(order) != 3 || order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Errorf("order = %v, want [first second third]", order)
	}
}

func TestRegistry_RunStopsOnError(t *testing.T) {
	reg := seed.NewRegistry()
	seedErr := errors.New("seed failed")

	s1 := &recordingSeeder{name: "ok"}
	s2 := &recordingSeeder{name: "bad", seedErr: seedErr}
	s3 := &recordingSeeder{name: "after-bad"}

	reg.Register(s1)
	reg.Register(s2)
	reg.Register(s3)

	result, err := reg.Run(context.Background(), seed.Deps{Logger: stubLogger{}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, seedErr) {
		t.Errorf("error = %v, want wrapped %v", err, seedErr)
	}
	if result.Executed != 1 {
		t.Errorf("Executed = %d, want 1 (first seeder only)", result.Executed)
	}
	if !s1.called {
		t.Error("s1 should have been called")
	}
	if !s2.called {
		t.Error("s2 should have been called")
	}
	if s3.called {
		t.Error("s3 should NOT have been called after error")
	}
}

func TestRegistry_DuplicateNamePanics(t *testing.T) {
	reg := seed.NewRegistry()
	reg.Register(seed.NewSeederFunc("dup", func(_ context.Context, _ seed.Deps) error { return nil }))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate name")
		}
	}()
	reg.Register(seed.NewSeederFunc("dup", func(_ context.Context, _ seed.Deps) error { return nil }))
}

func TestRegistry_EmptyNamePanics(t *testing.T) {
	reg := seed.NewRegistry()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on empty name")
		}
	}()
	reg.Register(seed.NewSeederFunc("", func(_ context.Context, _ seed.Deps) error { return nil }))
}

func TestSeederFunc(t *testing.T) {
	called := false
	s := seed.NewSeederFunc("test-seeder", func(_ context.Context, _ seed.Deps) error {
		called = true
		return nil
	})

	if s.Name() != "test-seeder" {
		t.Errorf("Name() = %q, want test-seeder", s.Name())
	}

	err := s.Seed(context.Background(), seed.Deps{Logger: stubLogger{}})
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	if !called {
		t.Error("function was not called")
	}
}

func TestRegistry_DepsPassed(t *testing.T) {
	reg := seed.NewRegistry()
	var receivedDeps seed.Deps

	reg.Register(seed.NewSeederFunc("check-deps", func(_ context.Context, deps seed.Deps) error {
		receivedDeps = deps
		return nil
	}))

	log := stubLogger{}
	_, err := reg.Run(context.Background(), seed.Deps{DB: nil, Logger: log})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if receivedDeps.DB != nil {
		t.Error("expected nil DB in test")
	}
}

func TestSeederFunc_NilFnPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil fn")
		}
	}()
	seed.NewSeederFunc("bad", nil)
}
