package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type bindTestQueue struct{ name string }

func (q *bindTestQueue) Enqueue(context.Context, jobs.Job) error    { return nil }
func (q *bindTestQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *bindTestQueue) Complete(context.Context, string) error     { return nil }
func (q *bindTestQueue) Fail(context.Context, string, error) error  { return nil }

// bindSchedulerQueue must return the plugin-registered instance (default
// core/postgres-queue path). Binding a different NewJobQueue-like instance
// must fail — that was the old runScheduler bug.
func TestBindSchedulerQueue_UsesPluginRegisteredQueue(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	registered := &bindTestQueue{name: "plugin"}
	app.RegisterQueue(registered)

	got, err := bindSchedulerQueue(app, nil, &config.Config{
		Queue: config.QueueConfig{Driver: "postgres"},
	})
	if err != nil {
		t.Fatalf("bindSchedulerQueue: %v", err)
	}
	if got != registered {
		t.Fatal("bindSchedulerQueue must return the plugin-registered queue")
	}
}

func TestBindSchedulerQueue_RejectsMismatchedNewQueue(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	app.RegisterQueue(&bindTestQueue{name: "plugin"})

	// Simulate the pre-fix runScheduler sequence: RegisterQueue during Init,
	// then BindQueue(postgres.NewJobQueue(...)) with a different instance.
	err := app.BindQueue(&bindTestQueue{name: "fresh-postgres"})
	if err == nil || !strings.Contains(err.Error(), "must match plugin-registered queue") {
		t.Fatalf("err = %v, want match plugin-registered queue", err)
	}
}
