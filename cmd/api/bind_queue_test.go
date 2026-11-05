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

// Mirrors runScheduler / schedule_cli after InitAll with core/postgres-queue
// active: a plugin already RegisterQueue'd, then resolve returns that instance
// and BindQueue must succeed (the old NewJobQueue+BindQueue path failed here).
func TestBindQueue_AfterPluginRegisterQueue_MatchesResolve(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	registered := &bindTestQueue{name: "plugin"}
	app.RegisterQueue(registered)

	resolved, err := resolveJobQueue(app, nil, &config.Config{
		Queue: config.QueueConfig{Driver: "postgres"},
	})
	if err != nil {
		t.Fatalf("resolveJobQueue: %v", err)
	}
	if resolved != registered {
		t.Fatal("resolveJobQueue must return the plugin-registered queue")
	}
	if err := app.BindQueue(resolved); err != nil {
		t.Fatalf("BindQueue after RegisterQueue: %v", err)
	}
	// A second distinct Postgres queue (the pre-fix runScheduler bug) must
	// not replace the bound enqueue queue.
	if err := app.BindQueue(&bindTestQueue{name: "other"}); err == nil {
		t.Fatal("expected BindQueue to reject a different enqueue instance")
	}
}
