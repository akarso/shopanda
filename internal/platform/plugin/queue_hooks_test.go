package plugin_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type hookQueue struct {
	stubQueue
	requeued []string
}

func (q *hookQueue) Requeue(_ context.Context, id string) error {
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
	got := app.ApplyQueueBoundHooks(inner)
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
	if err := q.Requeue(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	if len(inner.requeued) != 1 || inner.requeued[0] != "job-1" {
		t.Fatalf("inner requeued = %v", inner.requeued)
	}
	if len(seen) != 1 || seen[0] != "job-1" {
		t.Fatalf("OnRequeued = %v", seen)
	}
}
