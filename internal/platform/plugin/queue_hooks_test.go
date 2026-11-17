package plugin_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type hookQueue struct {
	stubQueue
	requeued   []string
	requeueErr error
}

func (q *hookQueue) Requeue(_ context.Context, id string, _ int) error {
	if q.requeueErr != nil {
		return q.requeueErr
	}
	q.requeued = append(q.requeued, id)
	return nil
}

func TestApp_OnQueueBound_AppliesInOrder(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	var order []string
	app.OnQueueBound(func(q jobs.Queue) jobs.Queue {
		order = append(order, "a")
		return q
	})
	app.OnQueueBound(func(q jobs.Queue) jobs.Queue {
		order = append(order, "b")
		return q
	})
	got, err := app.ApplyQueueBoundHooks(inner)
	if err != nil {
		t.Fatal(err)
	}
	if got != inner {
		t.Fatal("expected same queue when hooks are pass-through")
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v", order)
	}
}

func TestApp_WrapRequeueNotify_FiresOnRequeued(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	var seen []string
	app.OnRequeued(func(_ context.Context, jobID string) {
		seen = append(seen, jobID)
	})
	q := app.WrapRequeueNotify(inner)
	if err := q.Requeue(context.Background(), "job-1", 1); err != nil {
		t.Fatal(err)
	}
	if len(inner.requeued) != 1 || inner.requeued[0] != "job-1" {
		t.Fatalf("inner requeued = %v", inner.requeued)
	}
	if len(seen) != 1 || seen[0] != "job-1" {
		t.Fatalf("OnRequeued = %v", seen)
	}
}

func TestApp_WrapRequeueNotify_QuietOnError(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	inner.requeueErr = jobs.ErrRequeueConflict
	called := false
	app.OnRequeued(func(context.Context, string) { called = true })
	q := app.WrapRequeueNotify(inner)
	if err := q.Requeue(context.Background(), "job-1", 1); err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("OnRequeued must not run when Requeue fails")
	}
}

func TestApp_WrapRequeueNotify_RecoversHookPanic(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	app.OnRequeued(func(context.Context, string) { panic("boom") })
	q := app.WrapRequeueNotify(inner)
	if err := q.Requeue(context.Background(), "job-1", 1); err != nil {
		t.Fatalf("Requeue must still succeed after hook panic: %v", err)
	}
}

func TestApp_ApplyQueueBoundHooks_NilReturnsError(t *testing.T) {
	app := &plugin.App{}
	app.OnQueueBound(func(jobs.Queue) jobs.Queue { return nil })
	_, err := app.ApplyQueueBoundHooks(&hookQueue{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApp_ApplyQueueBoundHooks_PanicReturnsError(t *testing.T) {
	app := &plugin.App{}
	app.OnQueueBound(func(jobs.Queue) jobs.Queue { panic("bad hook") })
	_, err := app.ApplyQueueBoundHooks(&hookQueue{})
	if err == nil {
		t.Fatal("expected error")
	}
}

type introspectableHookQueue struct {
	hookQueue
}

func (introspectableHookQueue) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (introspectableHookQueue) Get(context.Context, string) (*jobs.Detail, error) { return nil, nil }
func (introspectableHookQueue) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (introspectableHookQueue) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (introspectableHookQueue) Retry(context.Context, string) error  { return nil }
func (introspectableHookQueue) Cancel(context.Context, string) error { return nil }

func TestApp_ApplyQueueBoundHooks_RejectsDroppedReader(t *testing.T) {
	app := &plugin.App{}
	app.OnQueueBound(func(q jobs.Queue) jobs.Queue {
		type embedOnly struct{ jobs.Queue }
		return &embedOnly{Queue: q}
	})
	_, err := app.ApplyQueueBoundHooks(&introspectableHookQueue{})
	if err == nil {
		t.Fatal("expected error when hook drops Reader")
	}
}

func TestApp_WrapRequeueNotify_KeepsReaderAdmin(t *testing.T) {
	app := &plugin.App{}
	inner := &introspectableHookQueue{}
	q := app.WrapRequeueNotify(inner)
	if _, ok := q.(jobs.Reader); !ok {
		t.Fatalf("%T must implement Reader", q)
	}
	if _, ok := q.(jobs.Admin); !ok {
		t.Fatalf("%T must implement Admin", q)
	}
	if _, err := q.(jobs.Reader).CountsByStatus(context.Background()); err != nil {
		t.Fatalf("CountsByStatus: %v", err)
	}
}

func TestApp_WrapRequeueNotify_QueueOnlyStaysQueueOnly(t *testing.T) {
	app := &plugin.App{}
	q := app.WrapRequeueNotify(&hookQueue{})
	if _, ok := q.(jobs.Reader); ok {
		t.Fatal("Queue-only wrap must not implement Reader")
	}
}
