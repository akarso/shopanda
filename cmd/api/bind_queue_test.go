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

func (q *bindTestQueue) Enqueue(context.Context, jobs.Job) error    { return nil }
func (q *bindTestQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *bindTestQueue) Complete(context.Context, string) error     { return nil }
func (q *bindTestQueue) Fail(context.Context, string, error) error  { return nil }

// resolveAndBindJobQueue must return the plugin-registered instance (default
// core/postgres-queue path used by serve/worker/scheduler/CLIs).
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
	if got != registered {
		t.Fatal("resolveAndBindJobQueue must return the plugin-registered queue")
	}
}
