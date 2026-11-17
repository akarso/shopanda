package plugin

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// QueueBoundHook wraps the composition-root queue after resolve and before
// BindQueue. Products use this to install a claim fence without editing
// cmd/api. Hooks run in registration order; each receives the previous
// wrapper's result.
type QueueBoundHook func(jobs.Queue) jobs.Queue

// RequeuedHook is invoked after Queue.Requeue succeeds (stale processing
// reclaim). Not called for Admin.Retry (failed → pending).
type RequeuedHook func(ctx context.Context, jobID string)

// OnQueueBound registers a wrap hook applied in resolveAndBindJobQueue.
// Safe to call during plugin Init.
func (a *App) OnQueueBound(hook QueueBoundHook) {
	if a == nil || hook == nil {
		return
	}
	a.queueBoundHooks = append(a.queueBoundHooks, hook)
}

// OnRequeued registers a callback fired when Requeue succeeds on the
// bound queue (via the notify wrapper installed at bind time).
func (a *App) OnRequeued(hook RequeuedHook) {
	if a == nil || hook == nil {
		return
	}
	a.requeuedHooks = append(a.requeuedHooks, hook)
}

// ApplyQueueBoundHooks runs OnQueueBound wrappers in order.
func (a *App) ApplyQueueBoundHooks(q jobs.Queue) jobs.Queue {
	if a == nil || q == nil {
		return q
	}
	for _, hook := range a.queueBoundHooks {
		if hook == nil {
			continue
		}
		next := hook(q)
		if next == nil {
			panic("plugin: OnQueueBound hook returned nil queue")
		}
		q = next
	}
	return q
}

// WrapRequeueNotify returns q wrapped so successful Requeue calls fire
// OnRequeued hooks.
func (a *App) WrapRequeueNotify(q jobs.Queue) jobs.Queue {
	if a == nil || q == nil || len(a.requeuedHooks) == 0 {
		return q
	}
	hooks := append([]RequeuedHook(nil), a.requeuedHooks...)
	return &requeueNotifyQueue{Queue: q, hooks: hooks}
}

type requeueNotifyQueue struct {
	jobs.Queue
	hooks []RequeuedHook
}

func (q *requeueNotifyQueue) Requeue(ctx context.Context, id string) error {
	if q == nil || q.Queue == nil {
		return fmt.Errorf("plugin: requeue: queue not configured")
	}
	if err := q.Queue.Requeue(ctx, id); err != nil {
		return err
	}
	for _, h := range q.hooks {
		if h != nil {
			h(ctx, id)
		}
	}
	return nil
}
