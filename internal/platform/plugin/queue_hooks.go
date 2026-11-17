package plugin

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// QueueBoundHook wraps the composition-root queue after resolve and before
// BindQueue. Products use this to install a claim fence without editing
// cmd/api. Hooks run in registration order; each receives the previous
// wrapper's result. Wrappers that embed jobs.Queue for fencing must also
// forward jobs.Reader and jobs.Admin when the inner queue implements them
// (CLI retry/cancel/requeue share the bound instance).
type QueueBoundHook func(jobs.Queue) jobs.Queue

// RequeuedHook is invoked after Queue.Requeue succeeds (stale processing
// reclaim). Not called for Admin.Retry (failed → pending). Hooks run with
// panic recovery so a bad callback cannot skip the caller's audit path.
type RequeuedHook func(ctx context.Context, jobID string)

// OnQueueBound registers a wrap hook applied in resolveAndBindJobQueue.
// Safe to call during plugin Init (sequential). Registrations after bind
// are not applied to the already-bound instance.
func (a *App) OnQueueBound(hook QueueBoundHook) {
	if a == nil || hook == nil {
		return
	}
	a.queueBoundHooks = append(a.queueBoundHooks, hook)
}

// OnRequeued registers a callback fired when Requeue succeeds on the
// bound queue (via the notify wrapper installed at bind time).
// Registrations after WrapRequeueNotify are not visible to that wrapper.
func (a *App) OnRequeued(hook RequeuedHook) {
	if a == nil || hook == nil {
		return
	}
	a.requeuedHooks = append(a.requeuedHooks, hook)
}

// ApplyQueueBoundHooks runs OnQueueBound wrappers in order. Returns an error
// if a hook returns nil (does not panic — bind stays on the error path).
func (a *App) ApplyQueueBoundHooks(q jobs.Queue) (jobs.Queue, error) {
	if a == nil || q == nil {
		return q, nil
	}
	for i, hook := range a.queueBoundHooks {
		if hook == nil {
			continue
		}
		next := hook(q)
		if next == nil {
			return nil, fmt.Errorf("plugin: OnQueueBound hook %d returned nil queue", i)
		}
		q = next
	}
	return q, nil
}

// WrapRequeueNotify returns q wrapped so successful Requeue calls fire
// OnRequeued hooks. Forwards Reader/Admin when the inner queue implements
// them so CLI introspection and mutate paths share one bound value.
func (a *App) WrapRequeueNotify(q jobs.Queue) jobs.Queue {
	if a == nil || q == nil {
		return q
	}
	hooks := append([]RequeuedHook(nil), a.requeuedHooks...)
	w := &requeueNotifyQueue{inner: q, hooks: hooks, log: a.Logger}
	if r, ok := q.(jobs.Reader); ok {
		w.reader = r
	}
	if ad, ok := q.(jobs.Admin); ok {
		w.admin = ad
	}
	return w
}

type requeueNotifyQueue struct {
	inner  jobs.Queue
	hooks  []RequeuedHook
	reader jobs.Reader
	admin  jobs.Admin
	log    loggerLike
}

type loggerLike interface {
	Error(msg string, err error, fields map[string]interface{})
}

func (q *requeueNotifyQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	return q.inner.Enqueue(ctx, job)
}
func (q *requeueNotifyQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	return q.inner.Dequeue(ctx)
}
func (q *requeueNotifyQueue) Complete(ctx context.Context, id string, attempt int) error {
	return q.inner.Complete(ctx, id, attempt)
}
func (q *requeueNotifyQueue) Fail(ctx context.Context, id string, attempt int, jobErr error) error {
	return q.inner.Fail(ctx, id, attempt, jobErr)
}

func (q *requeueNotifyQueue) Requeue(ctx context.Context, id string, attempt int) error {
	if q == nil || q.inner == nil {
		return fmt.Errorf("plugin: requeue: queue not configured")
	}
	if err := q.inner.Requeue(ctx, id, attempt); err != nil {
		return err
	}
	for _, h := range q.hooks {
		if h == nil {
			continue
		}
		func(hook RequeuedHook) {
			defer func() {
				if rec := recover(); rec != nil {
					// Requeue already committed; return success so the
					// caller (jobs:requeue) can still write audit.
					if q.log != nil {
						q.log.Error("plugin.queue.on_requeued_panic", fmt.Errorf("%v", rec), map[string]interface{}{
							"job_id": id,
						})
					}
				}
			}()
			hook(ctx, id)
		}(h)
	}
	return nil
}

func (q *requeueNotifyQueue) List(ctx context.Context, filter jobs.ListFilter) ([]jobs.Summary, error) {
	if q.reader == nil {
		return nil, fmt.Errorf("plugin: bound queue does not implement jobs.Reader")
	}
	return q.reader.List(ctx, filter)
}
func (q *requeueNotifyQueue) Get(ctx context.Context, id string) (*jobs.Detail, error) {
	if q.reader == nil {
		return nil, fmt.Errorf("plugin: bound queue does not implement jobs.Reader")
	}
	return q.reader.Get(ctx, id)
}
func (q *requeueNotifyQueue) CountsByType(ctx context.Context, jobType string) (map[jobs.Status]int, error) {
	if q.reader == nil {
		return nil, fmt.Errorf("plugin: bound queue does not implement jobs.Reader")
	}
	return q.reader.CountsByType(ctx, jobType)
}
func (q *requeueNotifyQueue) Retry(ctx context.Context, id string) error {
	if q.admin == nil {
		return fmt.Errorf("plugin: bound queue does not implement jobs.Admin")
	}
	return q.admin.Retry(ctx, id)
}
func (q *requeueNotifyQueue) Cancel(ctx context.Context, id string) error {
	if q.admin == nil {
		return fmt.Errorf("plugin: bound queue does not implement jobs.Admin")
	}
	return q.admin.Cancel(ctx, id)
}

var _ jobs.Queue = (*requeueNotifyQueue)(nil)
