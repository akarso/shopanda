package plugin_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type hookQueue struct {
	stubQueue
	requeued   []string
	requeueErr error
	beforeSeen []int
	lastOpts   jobs.RequeueSettings
}

func (q *hookQueue) Requeue(ctx context.Context, id string, attempt int, opts ...jobs.RequeueOption) error {
	return q.RequeueWithBeforeCommit(ctx, id, attempt, nil, opts...)
}

func (q *hookQueue) RequeueWithBeforeCommit(ctx context.Context, id string, attempt int, before jobs.RequeueBeforeCommit, opts ...jobs.RequeueOption) error {
	q.lastOpts = jobs.ResolveRequeueOptions(opts)
	if q.requeueErr != nil {
		return q.requeueErr
	}
	if before != nil {
		if err := before(ctx, id, attempt); err != nil {
			return err
		}
		q.beforeSeen = append(q.beforeSeen, attempt)
	}
	q.requeued = append(q.requeued, id)
	return nil
}

var _ jobs.RequeueHookable = (*hookQueue)(nil)

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
	var seenID string
	var seenAttempt int
	app.OnRequeued(func(_ context.Context, jobID string, attempt int) error {
		seenID = jobID
		seenAttempt = attempt
		return nil
	})
	q := app.WrapRequeueNotify(inner)
	if err := q.Requeue(context.Background(), "job-1", 3); err != nil {
		t.Fatal(err)
	}
	if len(inner.requeued) != 1 || inner.requeued[0] != "job-1" {
		t.Fatalf("inner requeued = %v", inner.requeued)
	}
	if len(inner.beforeSeen) != 1 || inner.beforeSeen[0] != 3 {
		t.Fatalf("beforeSeen = %v, want attempt 3 before pending", inner.beforeSeen)
	}
	if seenID != "job-1" || seenAttempt != 3 {
		t.Fatalf("OnRequeued = %s/%d", seenID, seenAttempt)
	}
}

func TestApp_WrapRequeueNotify_QuietOnError(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	inner.requeueErr = jobs.ErrRequeueConflict
	called := false
	app.OnRequeued(func(context.Context, string, int) error { called = true; return nil })
	q := app.WrapRequeueNotify(inner)
	if err := q.Requeue(context.Background(), "job-1", 1); err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("OnRequeued must not run when Requeue fails")
	}
}

func TestApp_WrapRequeueNotify_HookPanicFailsRequeue(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	app.OnRequeued(func(context.Context, string, int) error { panic("boom") })
	q := app.WrapRequeueNotify(inner)
	if err := q.Requeue(context.Background(), "job-1", 1); err == nil {
		t.Fatal("expected Requeue error after hook panic")
	}
	if len(inner.requeued) != 0 {
		t.Fatalf("pending must not flip after hook panic: %v", inner.requeued)
	}
}

func TestApp_WrapRequeueNotify_HookErrorFailsRequeue(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	want := errors.New("fence release failed")
	app.OnRequeued(func(context.Context, string, int) error { return want })
	q := app.WrapRequeueNotify(inner)
	err := q.Requeue(context.Background(), "job-1", 1)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if len(inner.requeued) != 0 {
		t.Fatal("pending must not flip after hook error")
	}
}

func TestApp_WrapRequeueNotify_TerminalOptionFiresOnRequeued(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	var seen bool
	app.OnRequeued(func(context.Context, string, int) error {
		seen = true
		return nil
	})
	q := app.WrapRequeueNotify(inner)
	if err := q.Requeue(context.Background(), "job-term", 2, jobs.WithRequeueTerminal()); err != nil {
		t.Fatal(err)
	}
	if !seen || !inner.lastOpts.Terminal || len(inner.requeued) != 1 {
		t.Fatalf("seen=%v terminal=%v requeued=%v", seen, inner.lastOpts.Terminal, inner.requeued)
	}
}

func TestApp_WrapRequeueNotify_TerminalHookErrorAborts(t *testing.T) {
	app := &plugin.App{}
	inner := &hookQueue{}
	want := errors.New("fence write failed")
	app.OnRequeued(func(context.Context, string, int) error { return want })
	q := app.WrapRequeueNotify(inner)
	err := q.Requeue(context.Background(), "job-term", 2, jobs.WithRequeueTerminal())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if len(inner.requeued) != 0 {
		t.Fatal("terminal reclaim must not commit after hook error")
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

func TestApp_ApplyQueueBoundHooks_TypedNilReturnsError(t *testing.T) {
	app := &plugin.App{}
	type fence struct{ jobs.Queue }
	app.OnQueueBound(func(jobs.Queue) jobs.Queue {
		var f *fence
		return f
	})
	_, err := app.ApplyQueueBoundHooks(&hookQueue{})
	if err == nil {
		t.Fatal("expected typed-nil bind error")
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
	retryN  atomic.Int32
	cancelN atomic.Int32
}

func (q *introspectableHookQueue) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (q *introspectableHookQueue) Get(context.Context, string) (*jobs.Detail, error) { return nil, nil }
func (q *introspectableHookQueue) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (q *introspectableHookQueue) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (q *introspectableHookQueue) Retry(context.Context, string) error {
	q.retryN.Add(1)
	return nil
}
func (q *introspectableHookQueue) Cancel(context.Context, string) error {
	q.cancelN.Add(1)
	return nil
}

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

func TestApp_WrapRequeueNotify_LiveAdminSameInstance(t *testing.T) {
	app := &plugin.App{}
	inner := &introspectableHookQueue{}
	q := app.WrapRequeueNotify(inner)
	ad := q.(jobs.Admin)
	if err := ad.Retry(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if err := q.Requeue(context.Background(), "y", 1); err != nil {
		t.Fatal(err)
	}
	if inner.retryN.Load() != 1 {
		t.Fatalf("Retry must hit live inner, got %d", inner.retryN.Load())
	}
	if len(inner.requeued) != 1 {
		t.Fatalf("Requeue must hit live inner, got %v", inner.requeued)
	}
}

func TestApp_WrapRequeueNotify_QueueOnlyStaysQueueOnly(t *testing.T) {
	app := &plugin.App{}
	q := app.WrapRequeueNotify(&hookQueue{})
	if _, ok := q.(jobs.Reader); ok {
		t.Fatal("Queue-only wrap must not implement Reader")
	}
}

func TestApp_WrapRequeueNotify_ValueWrapperWithFuncDoesNotPanic(t *testing.T) {
	app := &plugin.App{}
	inner := &introspectableHookQueue{}
	type fence struct {
		*introspectableHookQueue
		fn func()
	}
	wrapped := fence{
		introspectableHookQueue: inner,
		fn:                      func() {},
	}
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("WrapRequeueNotify panicked on unhashable queue value: %v", rec)
		}
	}()
	got := app.WrapRequeueNotify(wrapped)
	if _, ok := got.(jobs.Reader); !ok {
		t.Fatalf("%T must keep Reader through value fence", got)
	}
	// Retry and Requeue must resolve the same live inner (not a forked copy).
	if err := got.(jobs.Admin).Retry(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := got.Requeue(context.Background(), "b", 2); err != nil {
		t.Fatal(err)
	}
	if inner.retryN.Load() != 1 || len(inner.requeued) != 1 {
		t.Fatalf("live chain: retry=%d requeued=%v", inner.retryN.Load(), inner.requeued)
	}
}
