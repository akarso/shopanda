package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type testLogger struct{}

func (testLogger) Info(_ string, _ map[string]interface{})           {}
func (testLogger) Error(_ string, _ error, _ map[string]interface{}) {}

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

	if !afterPanic.Load() {
		t.Error("task after panic should still have fired")
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
