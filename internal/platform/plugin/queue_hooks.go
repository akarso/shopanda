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
// if a hook returns nil, panics, or drops jobs.Reader / jobs.Admin that the
// previous value implemented (bind stays on the error path — no panic).
func (a *App) ApplyQueueBoundHooks(q jobs.Queue) (jobs.Queue, error) {
	if a == nil || q == nil {
		return q, nil
	}
	for i, hook := range a.queueBoundHooks {
		if hook == nil {
			continue
		}
		hadReader := false
		hadAdmin := false
		if _, ok := q.(jobs.Reader); ok {
			hadReader = true
		}
		if _, ok := q.(jobs.Admin); ok {
			hadAdmin = true
		}
		var next jobs.Queue
		var panicErr error
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					panicErr = fmt.Errorf("plugin: OnQueueBound hook %d panicked: %v", i, rec)
				}
			}()
			next = hook(q)
		}()
		if panicErr != nil {
			return nil, panicErr
		}
		if next == nil {
			return nil, fmt.Errorf("plugin: OnQueueBound hook %d returned nil queue", i)
		}
		if hadReader {
			if _, ok := next.(jobs.Reader); !ok {
				return nil, fmt.Errorf("plugin: OnQueueBound hook %d dropped jobs.Reader — wrappers must forward List/Get/CountsByStatus/CountsByType", i)
			}
		}
		if hadAdmin {
			if _, ok := next.(jobs.Admin); !ok {
				return nil, fmt.Errorf("plugin: OnQueueBound hook %d dropped jobs.Admin — wrappers must forward Retry/Cancel", i)
			}
		}
		q = next
	}
	return q, nil
}

// queueUnwrapper lets bind wrappers expose the next inner queue so
// Reader/Admin resolution can walk past notify/fence layers.
type queueUnwrapper interface {
	UnwrapQueue() jobs.Queue
}

func skipNotifyWrapper(q jobs.Queue) (jobs.Queue, bool) {
	switch w := q.(type) {
	case *requeueNotifyQueue:
		return w.inner, true
	case *requeueNotifyIntrospectable:
		return w.inner, true
	default:
		return q, false
	}
}

func resolveReader(q jobs.Queue) jobs.Reader {
	seen := map[jobs.Queue]struct{}{}
	for q != nil {
		if _, ok := seen[q]; ok {
			return nil
		}
		seen[q] = struct{}{}
		if next, ok := skipNotifyWrapper(q); ok {
			q = next
			continue
		}
		if r, ok := q.(jobs.Reader); ok {
			return r
		}
		u, ok := q.(queueUnwrapper)
		if !ok {
			return nil
		}
		q = u.UnwrapQueue()
	}
	return nil
}

func resolveAdmin(q jobs.Queue) jobs.Admin {
	seen := map[jobs.Queue]struct{}{}
	for q != nil {
		if _, ok := seen[q]; ok {
			return nil
		}
		seen[q] = struct{}{}
		if next, ok := skipNotifyWrapper(q); ok {
			q = next
			continue
		}
		if ad, ok := q.(jobs.Admin); ok {
			return ad
		}
		u, ok := q.(queueUnwrapper)
		if !ok {
			return nil
		}
		q = u.UnwrapQueue()
	}
	return nil
}

// WrapRequeueNotify returns q wrapped so successful Requeue calls fire
// OnRequeued hooks. When the wrap chain exposes Reader and Admin, the
// returned value implements those ports too (postgres path). Broker queues
// without introspection stay Queue-only so serve still fails the Reader
// assert clearly instead of binding a nil-delegate Reader.
func (a *App) WrapRequeueNotify(q jobs.Queue) jobs.Queue {
	if a == nil || q == nil {
		return q
	}
	hooks := append([]RequeuedHook(nil), a.requeuedHooks...)
	base := requeueNotifyQueue{inner: q, hooks: hooks, log: a.Logger}
	reader := resolveReader(q)
	admin := resolveAdmin(q)
	if reader != nil && admin != nil {
		return &requeueNotifyIntrospectable{
			requeueNotifyQueue: base,
			reader:             reader,
			admin:              admin,
		}
	}
	return &base
}

// requeueNotifyQueue is Queue-only (OnRequeued fan-out). Used when the
// inner chain has no Reader/Admin (broker backends).
type requeueNotifyQueue struct {
	inner jobs.Queue
	hooks []RequeuedHook
	log   loggerLike
}

func (q *requeueNotifyQueue) UnwrapQueue() jobs.Queue {
	if q == nil {
		return nil
	}
	return q.inner
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

// requeueNotifyIntrospectable is the postgres bind shape: Queue + Reader +
// Admin forwarded from the wrap chain (including past OnQueueBound fences
// that themselves forward those ports).
type requeueNotifyIntrospectable struct {
	requeueNotifyQueue
	reader jobs.Reader
	admin  jobs.Admin
}

func (q *requeueNotifyIntrospectable) UnwrapQueue() jobs.Queue {
	if q == nil {
		return nil
	}
	return q.inner
}

func (q *requeueNotifyIntrospectable) List(ctx context.Context, filter jobs.ListFilter) ([]jobs.Summary, error) {
	return q.reader.List(ctx, filter)
}
func (q *requeueNotifyIntrospectable) Get(ctx context.Context, id string) (*jobs.Detail, error) {
	return q.reader.Get(ctx, id)
}
func (q *requeueNotifyIntrospectable) CountsByType(ctx context.Context, jobType string) (map[jobs.Status]int, error) {
	return q.reader.CountsByType(ctx, jobType)
}
func (q *requeueNotifyIntrospectable) CountsByStatus(ctx context.Context) (map[jobs.Status]int, error) {
	return q.reader.CountsByStatus(ctx)
}
func (q *requeueNotifyIntrospectable) Retry(ctx context.Context, id string) error {
	return q.admin.Retry(ctx, id)
}
func (q *requeueNotifyIntrospectable) Cancel(ctx context.Context, id string) error {
	return q.admin.Cancel(ctx, id)
}

var _ jobs.Queue = (*requeueNotifyQueue)(nil)
var _ jobs.Queue = (*requeueNotifyIntrospectable)(nil)
var _ jobs.Reader = (*requeueNotifyIntrospectable)(nil)
var _ jobs.Admin = (*requeueNotifyIntrospectable)(nil)
