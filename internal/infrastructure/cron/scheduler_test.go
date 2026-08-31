package cron

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type testLogger struct{}

func (testLogger) Info(_ string, _ map[string]interface{})           {}
func (testLogger) Error(_ string, _ error, _ map[string]interface{}) {}

// fakeStore is an in-memory scheduler.Store for testing WithStore
// integration without a real Postgres connection.
type fakeStore struct {
	mu            sync.Mutex
	registrations []scheduler.RegistrationEntry
	enabled       map[string]bool // absence means enabled, matching Store's documented default
	isEnabledErr  error
}

func (f *fakeStore) UpsertRegistrations(_ context.Context, entries []scheduler.RegistrationEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registrations = append([]scheduler.RegistrationEntry(nil), entries...)
	return nil
}

func (f *fakeStore) IsEnabledBatch(_ context.Context, names []string) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isEnabledErr != nil {
		return nil, f.isEnabledErr
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		if enabled, ok := f.enabled[name]; ok {
			out[name] = enabled
		}
	}
	return out, nil
}

func (f *fakeStore) snapshotRegistrations() []scheduler.RegistrationEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]scheduler.RegistrationEntry(nil), f.registrations...)
}

func TestScheduler_Register_Panics(t *testing.T) {
	s := New(testLogger{})

	// Empty name.
	assertPanics(t, "empty name", func() {
		s.Register("", "* * * * *", func() {})
	})

	// Nil function.
	assertPanics(t, "nil fn", func() {
		s.Register("test", "* * * * *", nil)
	})

	// Invalid spec.
	assertPanics(t, "bad spec", func() {
		s.Register("test", "bad", func() {})
	})

	// Duplicate name.
	s2 := New(testLogger{})
	s2.Register("dup", "* * * * *", func() {})
	assertPanics(t, "duplicate name", func() {
		s2.Register("dup", "* * * * *", func() {})
	})
}

func TestScheduler_StopIdempotent(t *testing.T) {
	s := New(testLogger{})
	// Calling Stop multiple times must not panic.
	s.Stop()
	s.Stop()
}

func TestScheduler_StartStop(t *testing.T) {
	s := New(testLogger{})
	s.Register("noop", "* * * * *", func() {})

	done := make(chan struct{})
	go func() {
		s.Start(context.Background())
		close(done)
	}()

	// Stop should unblock Start.
	s.Stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestScheduler_ContextCancel(t *testing.T) {
	s := New(testLogger{})
	s.Register("noop", "* * * * *", func() {})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestScheduler_Tick(t *testing.T) {
	// Test the tick method directly (no waiting for real minutes).
	var count atomic.Int32
	s := New(testLogger{})
	s.Register("every-minute", "* * * * *", func() {
		count.Add(1)
	})
	s.Register("never", "0 0 1 1 1", func() {
		t.Error("should not fire")
	})

	// Simulate a tick at an arbitrary time.
	now := time.Date(2026, 4, 7, 10, 30, 0, 0, time.UTC)
	s.tick(now)
	s.wg.Wait() // tasks run async

	if got := count.Load(); got != 1 {
		t.Errorf("expected 1 fire, got %d", got)
	}
}

func TestScheduler_TaskPanicRecovery(t *testing.T) {
	s := New(testLogger{})
	var afterPanic atomic.Bool
	s.Register("panic-task", "* * * * *", func() {
		panic("boom")
	})
	s.Register("after-task", "* * * * *", func() {
		afterPanic.Store(true)
	})

	now := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	s.tick(now)
	s.wg.Wait() // tasks run async

	if !afterPanic.Load() {
		t.Error("task after panic should still have fired")
	}
}

func TestNextRun(t *testing.T) {
	after := time.Date(2026, 4, 7, 10, 30, 0, 0, time.UTC)

	next, err := NextRun("0 * * * *", after)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 4, 7, 11, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v", next, want)
	}
}

func TestNextRun_StrictlyAfter(t *testing.T) {
	// A time that already matches "every minute" must still return the
	// *next* minute, not the same instant — NextRun documents "strictly
	// after".
	after := time.Date(2026, 4, 7, 10, 30, 0, 0, time.UTC)
	next, err := NextRun("* * * * *", after)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := after.Add(time.Minute)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v", next, want)
	}
}

func TestNextRun_InvalidSpec(t *testing.T) {
	if _, err := NextRun("bad spec", time.Now()); err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

func TestScheduler_Start_UpsertsRegistrations(t *testing.T) {
	store := &fakeStore{}
	s := New(testLogger{}, WithStore(store))
	s.Register("task-a", "* * * * *", func() {})
	s.Register("task-b", "0 3 * * *", func() {})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}

	got := store.snapshotRegistrations()
	if len(got) != 2 {
		t.Fatalf("registrations = %+v, want 2 entries", got)
	}
	byName := map[string]string{}
	for _, r := range got {
		byName[r.Name] = r.Spec
	}
	if byName["task-a"] != "* * * * *" || byName["task-b"] != "0 3 * * *" {
		t.Errorf("registrations = %+v, want task-a/task-b with their specs", byName)
	}
}

func TestScheduler_Tick_SkipsDisabledTask(t *testing.T) {
	var fired atomic.Bool
	store := &fakeStore{enabled: map[string]bool{"disabled-task": false}}
	s := New(testLogger{}, WithStore(store))
	s.Register("disabled-task", "* * * * *", func() { fired.Store(true) })

	s.tick(time.Date(2026, 4, 7, 10, 30, 0, 0, time.UTC))
	s.wg.Wait()

	if fired.Load() {
		t.Error("disabled task should not have fired")
	}
}

func TestScheduler_Tick_FiresEnabledTask(t *testing.T) {
	var fired atomic.Bool
	store := &fakeStore{enabled: map[string]bool{"enabled-task": true}}
	s := New(testLogger{}, WithStore(store))
	s.Register("enabled-task", "* * * * *", func() { fired.Store(true) })

	s.tick(time.Date(2026, 4, 7, 10, 30, 0, 0, time.UTC))
	s.wg.Wait()

	if !fired.Load() {
		t.Error("enabled task should have fired")
	}
}

// TestScheduler_Tick_FailsOpenOnStoreError pins the fail-open decision: a
// transient override-check failure must not silently stop scheduled work
// (e.g. reservation-expiry cleanup) — that failure mode is worse than
// occasionally firing a task an admin meant to keep disabled.
func TestScheduler_Tick_FailsOpenOnStoreError(t *testing.T) {
	var fired atomic.Bool
	store := &fakeStore{isEnabledErr: errors.New("db down")}
	s := New(testLogger{}, WithStore(store))
	s.Register("task", "* * * * *", func() { fired.Store(true) })

	s.tick(time.Date(2026, 4, 7, 10, 30, 0, 0, time.UTC))
	s.wg.Wait()

	if !fired.Load() {
		t.Error("task should still fire when the override check itself fails (fail open)")
	}
}

func TestScheduler_TriggerLocal_FiresRegisteredTask(t *testing.T) {
	var fired atomic.Bool
	s := New(testLogger{})
	s.Register("task", "0 0 1 1 1", func() { fired.Store(true) }) // never matches a real tick

	if err := s.TriggerLocal("task"); err != nil {
		t.Fatalf("TriggerLocal: %v", err)
	}
	s.wg.Wait()

	if !fired.Load() {
		t.Error("TriggerLocal should fire the task regardless of its cron spec")
	}
}

func TestScheduler_TriggerLocal_IgnoresDisabledOverride(t *testing.T) {
	var fired atomic.Bool
	store := &fakeStore{enabled: map[string]bool{"task": false}}
	s := New(testLogger{}, WithStore(store))
	s.Register("task", "* * * * *", func() { fired.Store(true) })

	if err := s.TriggerLocal("task"); err != nil {
		t.Fatalf("TriggerLocal: %v", err)
	}
	s.wg.Wait()

	if !fired.Load() {
		t.Error("TriggerLocal must fire even when the task is disabled — disabling only gates the automatic tick")
	}
}

func TestScheduler_TriggerLocal_NotFound(t *testing.T) {
	s := New(testLogger{})
	s.Register("task", "* * * * *", func() {})

	err := s.TriggerLocal("missing")
	if err == nil {
		t.Fatal("expected error for unregistered task")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("err = %v, want *apperror.Error{Code: not_found}", err)
	}
}

// TestScheduler_TriggerLocal_AfterStop_ReturnsConflictNotPanic pins the fix
// for a wg.Add/wg.Wait race: TriggerLocal (an externally-reachable call
// site via the admin API, unlike tick which only ever runs from
// Scheduler's own single Start loop) must not call wg.Add after Stop has
// already begun — sync.WaitGroup panics on "Add called concurrently with
// Wait" in that case. tryAdd's stopped check closes the race by refusing
// to Add once Stop has marked the scheduler stopped.
func TestScheduler_TriggerLocal_AfterStop_ReturnsConflictNotPanic(t *testing.T) {
	s := New(testLogger{})
	s.Register("task", "* * * * *", func() {})

	s.Stop() // no Start running, but stopOnce/stopped still take effect

	err := s.TriggerLocal("task")
	if err == nil {
		t.Fatal("expected an error triggering after Stop")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("err = %v, want *apperror.Error{Code: conflict}", err)
	}
}

// TestScheduler_Stop_ConcurrentWithTriggerLocal_NoPanic is a stress test
// for the same tryAdd race guard: repeatedly races TriggerLocal against
// Stop from separate goroutines. Run with -race to catch a reintroduced
// data race, and bare (no -race) to catch the WaitGroup misuse panic
// itself, which -race does not reliably surface.
func TestScheduler_Stop_ConcurrentWithTriggerLocal_NoPanic(t *testing.T) {
	for i := 0; i < 200; i++ {
		s := New(testLogger{})
		s.Register("task", "* * * * *", func() {})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.TriggerLocal("task")
		}()
		go func() {
			defer wg.Done()
			s.Stop()
		}()
		wg.Wait()
	}
}

func assertPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic, got none", name)
		}
	}()
	fn()
}
