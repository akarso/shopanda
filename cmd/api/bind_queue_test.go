package main

import (
	"context"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type bindTestQueue struct{ name string }

func (q *bindTestQueue) Enqueue(context.Context, jobs.Job) error          { return nil }
func (q *bindTestQueue) Dequeue(context.Context) (*jobs.Job, error)       { return nil, nil }
func (q *bindTestQueue) Complete(context.Context, string, int) error      { return nil }
func (q *bindTestQueue) Fail(context.Context, string, int, error) error   { return nil }
func (q *bindTestQueue) Requeue(context.Context, string, int) error       { return nil }

// bindTestIntrospectableQueue is the postgres-shaped port set.
type bindTestIntrospectableQueue struct {
	bindTestQueue
	listCalled bool
}

func (q *bindTestIntrospectableQueue) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	q.listCalled = true
	return nil, nil
}
func (q *bindTestIntrospectableQueue) Get(context.Context, string) (*jobs.Detail, error) {
	return nil, nil
}
func (q *bindTestIntrospectableQueue) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (q *bindTestIntrospectableQueue) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (q *bindTestIntrospectableQueue) Retry(context.Context, string) error  { return nil }
func (q *bindTestIntrospectableQueue) Cancel(context.Context, string) error { return nil }

type fenceQueue struct {
	jobs.Queue
	reader jobs.Reader
	admin  jobs.Admin
}

func (f *fenceQueue) UnwrapQueue() jobs.Queue { return f.Queue }

func (f *fenceQueue) List(ctx context.Context, filter jobs.ListFilter) ([]jobs.Summary, error) {
	return f.reader.List(ctx, filter)
}
func (f *fenceQueue) Get(ctx context.Context, id string) (*jobs.Detail, error) {
	return f.reader.Get(ctx, id)
}
func (f *fenceQueue) CountsByStatus(ctx context.Context) (map[jobs.Status]int, error) {
	return f.reader.CountsByStatus(ctx)
}
func (f *fenceQueue) CountsByType(ctx context.Context, jobType string) (map[jobs.Status]int, error) {
	return f.reader.CountsByType(ctx, jobType)
}
func (f *fenceQueue) Retry(ctx context.Context, id string) error {
	return f.admin.Retry(ctx, id)
}
func (f *fenceQueue) Cancel(ctx context.Context, id string) error {
	return f.admin.Cancel(ctx, id)
}

// resolveAndBindJobQueue must bind the plugin-registered instance (wrapped
// for OnRequeued). Queue-only backends stay Queue-only after wrap.
func TestResolveAndBindJobQueue_UsesPluginRegisteredQueue(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	registered := &bindTestQueue{name: "plugin"}
	app.RegisterQueue(registered)

	got, err := resolveAndBindJobQueue(app, nil, &config.Config{
		Queue: config.QueueConfig{Driver: "postgres"},
	})
	if err != nil {
		t.Fatalf("resolveAndBindJobQueue: %v", err)
	}
	if _, ok := got.(jobs.Reader); ok {
		t.Fatal("Queue-only backend must not gain a false Reader via notify wrap")
	}
	if err := got.Enqueue(context.Background(), jobs.Job{ID: "x", Type: "t"}); err != nil {
		t.Fatalf("Enqueue via bound queue: %v", err)
	}
}

func TestResolveAndBindJobQueue_PostgresShapeKeepsReaderAdmin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	registered := &bindTestIntrospectableQueue{bindTestQueue: bindTestQueue{name: "pg"}}
	app.RegisterQueue(registered)

	got, err := resolveAndBindJobQueue(app, nil, &config.Config{
		Queue: config.QueueConfig{Driver: "postgres"},
	})
	if err != nil {
		t.Fatalf("resolveAndBindJobQueue: %v", err)
	}
	reader, okR := got.(jobs.Reader)
	admin, okA := got.(jobs.Admin)
	if !okR || !okA {
		t.Fatalf("bound queue %T must implement Reader and Admin", got)
	}
	if _, err := reader.CountsByStatus(context.Background()); err != nil {
		t.Fatalf("CountsByStatus: %v", err)
	}
	if err := admin.Retry(context.Background(), "id"); err != nil {
		t.Fatalf("Retry: %v", err)
	}
}

func TestResolveAndBindJobQueue_OnQueueBoundFenceForwardsPorts(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	registered := &bindTestIntrospectableQueue{bindTestQueue: bindTestQueue{name: "pg"}}
	app.RegisterQueue(registered)
	app.OnQueueBound(func(q jobs.Queue) jobs.Queue {
		r, _ := q.(jobs.Reader)
		ad, _ := q.(jobs.Admin)
		return &fenceQueue{Queue: q, reader: r, admin: ad}
	})

	got, err := resolveAndBindJobQueue(app, nil, &config.Config{
		Queue: config.QueueConfig{Driver: "postgres"},
	})
	if err != nil {
		t.Fatalf("resolveAndBindJobQueue: %v", err)
	}
	if _, ok := got.(jobs.Reader); !ok {
		t.Fatalf("bound queue %T lost Reader after OnQueueBound fence", got)
	}
	if _, ok := got.(jobs.Admin); !ok {
		t.Fatalf("bound queue %T lost Admin after OnQueueBound fence", got)
	}
	if _, err := got.(jobs.Reader).List(context.Background(), jobs.ListFilter{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !registered.listCalled {
		t.Fatal("List must reach the inner postgres-shaped queue")
	}
}

func TestResolveAndBindJobQueue_OnQueueBoundDroppingReaderFailsBind(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	registered := &bindTestIntrospectableQueue{bindTestQueue: bindTestQueue{name: "pg"}}
	app.RegisterQueue(registered)
	app.OnQueueBound(func(q jobs.Queue) jobs.Queue {
		// Embed Queue only — drops Reader/Admin.
		type embedOnly struct{ jobs.Queue }
		return &embedOnly{Queue: q}
	})

	_, err := resolveAndBindJobQueue(app, nil, &config.Config{
		Queue: config.QueueConfig{Driver: "postgres"},
	})
	if err == nil {
		t.Fatal("expected bind error when OnQueueBound drops Reader")
	}
}
