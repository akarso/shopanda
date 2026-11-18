package plugin

import (
	"context"
	"fmt"
	"reflect"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// QueueBoundHook wraps the composition-root queue after resolve and before
// BindQueue. Products use this to install a claim fence without editing
// cmd/api. Hooks run in registration order; each receives the previous
// wrapper's result. Wrappers that embed jobs.Queue for fencing must also
// forward jobs.Reader and jobs.Admin when the inner queue implements them
// (CLI retry/cancel/requeue share the bound instance). Prefer pointer
// wrappers — value wrappers that hold funcs are copied when assigned and
// can fork mutex/fence state across Reader vs Requeue paths.
type QueueBoundHook func(jobs.Queue) jobs.Queue

// RequeuedHook runs under the reclaim lock after predicates pass and
// before status flips to pending (postgres RequeueHookable). attempt is
// the claim being released. A non-nil error or panic aborts Requeue —
// the row stays processing and jobs:requeue must not audit success.
// Not called for Admin.Retry (failed → pending).
type RequeuedHook func(ctx context.Context, jobID string, attempt int) error

// OnQueueBound registers a wrap hook applied in resolveAndBindJobQueue.
// Safe to call during plugin Init (sequential). Registrations after bind
// are not applied to the already-bound instance.
func (a *App) OnQueueBound(hook QueueBoundHook) {
	if a == nil || hook == nil {
		return
	}
	a.queueBoundHooks = append(a.queueBoundHooks, hook)
}

// OnRequeued registers a callback fired during Requeue (before pending)
// on backends that implement jobs.RequeueHookable. Registrations after
// WrapRequeueNotify are not visible to that wrapper.
func (a *App) OnRequeued(hook RequeuedHook) {
	if a == nil || hook == nil {
		return
	}
	a.requeuedHooks = append(a.requeuedHooks, hook)
}

// isNilQueue reports untyped nil or a typed-nil pointer/iface/map/etc
// inside a jobs.Queue interface value (bind must fail closed).
func isNilQueue(q jobs.Queue) bool {
	if q == nil {
		return true
	}
	v := reflect.ValueOf(q)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

// ApplyQueueBoundHooks runs OnQueueBound wrappers in order. Returns an error
// if a hook returns nil (including typed nil), panics, or drops
// jobs.Reader / jobs.Admin that the previous value implemented.
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
		if isNilQueue(next) {
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

// maxQueueUnwrapDepth bounds wrap-chain walks. Do not use map[jobs.Queue]:
// a value wrapper that holds a func/slice/map is not comparable and panics
// on map insert (serve/worker/jobs:* bind crash).
const maxQueueUnwrapDepth = 32

func resolveReader(q jobs.Queue) jobs.Reader {
	for depth := 0; q != nil && depth < maxQueueUnwrapDepth; depth++ {
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
		next := u.UnwrapQueue()
		if next == nil || queueSame(next, q) {
			return nil
		}
		q = next
	}
	return nil
}

func resolveAdmin(q jobs.Queue) jobs.Admin {
	for depth := 0; q != nil && depth < maxQueueUnwrapDepth; depth++ {
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
		next := u.UnwrapQueue()
		if next == nil || queueSame(next, q) {
			return nil
		}
		q = next
	}
	return nil
}

// WrapRequeueNotify returns q wrapped so Requeue runs OnRequeued hooks
// before pending (via jobs.RequeueHookable). When the wrap chain exposes
// Reader and Admin at wrap time, the returned value implements those
// ports by resolving the live chain on each call (avoids snapshotting a
// copied value-type fence).
func (a *App) WrapRequeueNotify(q jobs.Queue) jobs.Queue {
	if a == nil || q == nil {
		return q
	}
	hooks := append([]RequeuedHook(nil), a.requeuedHooks...)
	base := &requeueNotifyQueue{inner: q, hooks: hooks, log: a.Logger}
	if resolveReader(q) != nil && resolveAdmin(q) != nil {
		return &requeueNotifyIntrospectable{requeueNotifyQueue: base}
	}
	return base
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

func (q *requeueNotifyQueue) runRequeuedHooks(ctx context.Context, id string, attempt int) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("plugin: OnRequeued panic: %v", rec)
			if q.log != nil {
				q.log.Error("plugin.queue.on_requeued_panic", err, map[string]interface{}{
					"job_id":  id,
					"attempt": attempt,
				})
			}
		}
	}()
	for _, h := range q.hooks {
		if h == nil {
			continue
		}
		if hookErr := h(ctx, id, attempt); hookErr != nil {
			return hookErr
		}
	}
	return nil
}

func (q *requeueNotifyQueue) Requeue(ctx context.Context, id string, attempt int, opts ...jobs.RequeueOption) error {
	if q == nil || q.inner == nil {
		return fmt.Errorf("plugin: requeue: queue not configured")
	}
	before := jobs.RequeueBeforeCommit(func(ctx context.Context, jobID string, att int) error {
		return q.runRequeuedHooks(ctx, jobID, att)
	})
	if h, ok := q.inner.(jobs.RequeueHookable); ok {
		return h.RequeueWithBeforeCommit(ctx, id, attempt, before, opts...)
	}
	// Non-hookable backends (brokers / stubs): Requeue first; hooks only
	// on success. Postgres always implements RequeueHookable.
	if err := q.inner.Requeue(ctx, id, attempt, opts...); err != nil {
		return err
	}
	if err := q.runRequeuedHooks(ctx, id, attempt); err != nil {
		return err
	}
	return nil
}

// requeueNotifyIntrospectable is the postgres bind shape: Queue + Reader +
// Admin resolved from the live wrap chain on each call.
type requeueNotifyIntrospectable struct {
	*requeueNotifyQueue
}

func (q *requeueNotifyIntrospectable) UnwrapQueue() jobs.Queue {
	if q == nil || q.requeueNotifyQueue == nil {
		return nil
	}
	return q.inner
}

func (q *requeueNotifyIntrospectable) liveReader() (jobs.Reader, error) {
	r := resolveReader(q.inner)
	if r == nil {
		return nil, fmt.Errorf("plugin: queue wrap chain lost jobs.Reader")
	}
	return r, nil
}

func (q *requeueNotifyIntrospectable) liveAdmin() (jobs.Admin, error) {
	ad := resolveAdmin(q.inner)
	if ad == nil {
		return nil, fmt.Errorf("plugin: queue wrap chain lost jobs.Admin")
	}
	return ad, nil
}

func (q *requeueNotifyIntrospectable) List(ctx context.Context, filter jobs.ListFilter) ([]jobs.Summary, error) {
	r, err := q.liveReader()
	if err != nil {
		return nil, err
	}
	return r.List(ctx, filter)
}
func (q *requeueNotifyIntrospectable) Get(ctx context.Context, id string) (*jobs.Detail, error) {
	r, err := q.liveReader()
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}
func (q *requeueNotifyIntrospectable) CountsByType(ctx context.Context, jobType string) (map[jobs.Status]int, error) {
	r, err := q.liveReader()
	if err != nil {
		return nil, err
	}
	return r.CountsByType(ctx, jobType)
}
func (q *requeueNotifyIntrospectable) CountsByStatus(ctx context.Context) (map[jobs.Status]int, error) {
	r, err := q.liveReader()
	if err != nil {
		return nil, err
	}
	return r.CountsByStatus(ctx)
}
func (q *requeueNotifyIntrospectable) Retry(ctx context.Context, id string) error {
	ad, err := q.liveAdmin()
	if err != nil {
		return err
	}
	return ad.Retry(ctx, id)
}
func (q *requeueNotifyIntrospectable) Cancel(ctx context.Context, id string) error {
	ad, err := q.liveAdmin()
	if err != nil {
		return err
	}
	return ad.Cancel(ctx, id)
}

var _ jobs.Queue = (*requeueNotifyQueue)(nil)
var _ jobs.Queue = (*requeueNotifyIntrospectable)(nil)
var _ jobs.Reader = (*requeueNotifyIntrospectable)(nil)
var _ jobs.Admin = (*requeueNotifyIntrospectable)(nil)
