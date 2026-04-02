package event_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func testLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

// ── Event envelope tests ────────────────────────────────────────────────

func TestNew(t *testing.T) {
	evt := event.New("catalog.product.created", "core.catalog", map[string]string{"product_id": "p1"})

	if evt.ID == "" {
		t.Error("expected non-empty ID")
	}
	if evt.Name != "catalog.product.created" {
		t.Errorf("Name = %q, want catalog.product.created", evt.Name)
	}
	if evt.Version != 1 {
		t.Errorf("Version = %d, want 1", evt.Version)
	}
	if evt.Source != "core.catalog" {
		t.Errorf("Source = %q, want core.catalog", evt.Source)
	}
	if evt.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
	if evt.Meta != nil {
		t.Errorf("Meta = %v, want nil", evt.Meta)
	}
}

func TestEvent_WithMeta(t *testing.T) {
	evt := event.New("test.event", "test", nil)
	evt2 := evt.WithMeta("request_id", "req-1").WithMeta("user_id", "u-1")

	if evt.Meta != nil {
		t.Error("original event should not be mutated")
	}
	if evt2.Meta["request_id"] != "req-1" {
		t.Errorf("request_id = %q, want req-1", evt2.Meta["request_id"])
	}
	if evt2.Meta["user_id"] != "u-1" {
		t.Errorf("user_id = %q, want u-1", evt2.Meta["user_id"])
	}
}

func TestEvent_WithVersion(t *testing.T) {
	evt := event.New("test.event", "test", nil)
	evt2 := evt.WithVersion(3)

	if evt.Version != 1 {
		t.Errorf("original Version = %d, want 1", evt.Version)
	}
	if evt2.Version != 3 {
		t.Errorf("Version = %d, want 3", evt2.Version)
	}
}

func TestEvent_WithVersion_NonPositive(t *testing.T) {
	evt := event.New("test.event", "test", nil)

	if got := evt.WithVersion(0); got.Version != 1 {
		t.Errorf("WithVersion(0).Version = %d, want 1", got.Version)
	}
	if got := evt.WithVersion(-1); got.Version != 1 {
		t.Errorf("WithVersion(-1).Version = %d, want 1", got.Version)
	}
}

func TestEvent_WithMeta_ChainedImmutability(t *testing.T) {
	evt1 := event.New("test.event", "test", nil).WithMeta("a", "1")
	evt2 := evt1.WithMeta("b", "2")

	if evt1.Meta["a"] != "1" {
		t.Errorf("evt1 missing a, got %v", evt1.Meta)
	}
	if _, ok := evt1.Meta["b"]; ok {
		t.Errorf("evt1 should not contain b, got %v", evt1.Meta)
	}
	if evt2.Meta["a"] != "1" {
		t.Errorf("evt2 missing a, got %v", evt2.Meta)
	}
	if evt2.Meta["b"] != "2" {
		t.Errorf("evt2 missing b, got %v", evt2.Meta)
	}
}

// ── Bus tests ───────────────────────────────────────────────────────────

func TestNewBus_NilLogger_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	event.NewBus(nil)
}

func TestBus_On_NilHandler_Panics(t *testing.T) {
	bus := event.NewBus(testLogger())
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil handler")
		}
	}()
	bus.On("test.event", nil)
}

func TestBus_OnAsync_NilHandler_Panics(t *testing.T) {
	bus := event.NewBus(testLogger())
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil handler")
		}
	}()
	bus.OnAsync("test.event", nil)
}

func TestBus_SyncHandler(t *testing.T) {
	bus := event.NewBus(testLogger())
	var called bool

	bus.On("test.happened", func(_ context.Context, evt event.Event) error {
		called = true
		if evt.Name != "test.happened" {
			t.Errorf("event name = %q", evt.Name)
		}
		return nil
	})

	evt := event.New("test.happened", "test", nil)
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !called {
		t.Error("sync handler was not called")
	}
}

func TestBus_SyncHandler_Order(t *testing.T) {
	bus := event.NewBus(testLogger())
	var order []int

	bus.On("test.order", func(_ context.Context, _ event.Event) error {
		order = append(order, 1)
		return nil
	})
	bus.On("test.order", func(_ context.Context, _ event.Event) error {
		order = append(order, 2)
		return nil
	})
	bus.On("test.order", func(_ context.Context, _ event.Event) error {
		order = append(order, 3)
		return nil
	})

	evt := event.New("test.order", "test", nil)
	bus.Publish(context.Background(), evt)

	if len(order) != 3 {
		t.Fatalf("len = %d, want 3", len(order))
	}
	for i, v := range order {
		if v != i+1 {
			t.Errorf("order[%d] = %d, want %d", i, v, i+1)
		}
	}
}

func TestBus_SyncHandler_Error_Aborts(t *testing.T) {
	bus := event.NewBus(testLogger())
	handlerErr := errors.New("sync failed")
	var secondCalled bool

	bus.On("test.fail", func(_ context.Context, _ event.Event) error {
		return handlerErr
	})
	bus.On("test.fail", func(_ context.Context, _ event.Event) error {
		secondCalled = true
		return nil
	})

	evt := event.New("test.fail", "test", nil)
	err := bus.Publish(context.Background(), evt)
	if !errors.Is(err, handlerErr) {
		t.Errorf("err = %v, want %v", err, handlerErr)
	}
	if secondCalled {
		t.Error("second handler should not have been called")
	}
}

func TestBus_SyncError_SkipsAsync(t *testing.T) {
	bus := event.NewBus(testLogger())
	var asyncCalled atomic.Bool

	bus.On("test.fail", func(_ context.Context, _ event.Event) error {
		return errors.New("boom")
	})
	bus.OnAsync("test.fail", func(_ context.Context, _ event.Event) error {
		asyncCalled.Store(true)
		return nil
	})

	evt := event.New("test.fail", "test", nil)
	bus.Publish(context.Background(), evt)

	// Give async a chance to fire (it should not).
	time.Sleep(50 * time.Millisecond)
	if asyncCalled.Load() {
		t.Error("async handler should not run when sync handler fails")
	}
}

func TestBus_AsyncHandler(t *testing.T) {
	bus := event.NewBus(testLogger())
	done := make(chan struct{})

	bus.OnAsync("test.async", func(_ context.Context, evt event.Event) error {
		if evt.Name != "test.async" {
			t.Errorf("event name = %q", evt.Name)
		}
		close(done)
		return nil
	})

	evt := event.New("test.async", "test", nil)
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("async handler did not run within timeout")
	}
}

func TestBus_AsyncHandler_Error_DoesNotPropagate(t *testing.T) {
	bus := event.NewBus(testLogger())
	done := make(chan struct{})

	bus.OnAsync("test.async.err", func(_ context.Context, _ event.Event) error {
		defer close(done)
		return errors.New("async error")
	})

	evt := event.New("test.async.err", "test", nil)
	err := bus.Publish(context.Background(), evt)
	if err != nil {
		t.Fatalf("Publish should not return async errors, got: %v", err)
	}

	select {
	case <-done:
		// success — handler ran and errored, but Publish didn\'t fail
	case <-time.After(2 * time.Second):
		t.Fatal("async handler did not run within timeout")
	}
}

func TestBus_NoHandlers(t *testing.T) {
	bus := event.NewBus(testLogger())

	evt := event.New("nobody.listening", "test", nil)
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish with no handlers: %v", err)
	}
}

func TestBus_Handlers_Count(t *testing.T) {
	bus := event.NewBus(testLogger())
	noop := func(_ context.Context, _ event.Event) error { return nil }

	bus.On("test.count", noop)
	bus.On("test.count", noop)
	bus.OnAsync("test.count", noop)

	if got := bus.Handlers("test.count"); got != 3 {
		t.Errorf("Handlers = %d, want 3", got)
	}
	if got := bus.Handlers("test.other"); got != 0 {
		t.Errorf("Handlers(other) = %d, want 0", got)
	}
}

func TestBus_MixedSyncAsync(t *testing.T) {
	bus := event.NewBus(testLogger())
	var syncOrder []int
	asyncDone := make(chan struct{})

	bus.On("test.mixed", func(_ context.Context, _ event.Event) error {
		syncOrder = append(syncOrder, 1)
		return nil
	})
	bus.On("test.mixed", func(_ context.Context, _ event.Event) error {
		syncOrder = append(syncOrder, 2)
		return nil
	})
	bus.OnAsync("test.mixed", func(_ context.Context, _ event.Event) error {
		close(asyncDone)
		return nil
	})

	evt := event.New("test.mixed", "test", nil)
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Sync handlers already ran.
	if len(syncOrder) != 2 || syncOrder[0] != 1 || syncOrder[1] != 2 {
		t.Errorf("syncOrder = %v, want [1 2]", syncOrder)
	}

	select {
	case <-asyncDone:
	case <-time.After(2 * time.Second):
		t.Fatal("async handler timed out")
	}
}

func TestBus_EventDataAccess(t *testing.T) {
	bus := event.NewBus(testLogger())

	type payload struct {
		ProductID string
	}

	var received payload
	bus.On("catalog.product.created", func(_ context.Context, evt event.Event) error {
		p, ok := evt.Data.(payload)
		if !ok {
			t.Fatal("unexpected data type")
		}
		received = p
		return nil
	})

	evt := event.New("catalog.product.created", "core.catalog", payload{ProductID: "p-1"})
	bus.Publish(context.Background(), evt)

	if received.ProductID != "p-1" {
		t.Errorf("ProductID = %q, want p-1", received.ProductID)
	}
}
