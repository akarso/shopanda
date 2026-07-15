package plugin_test

import (
	"context"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestApp_Integration_RegisterSyncJob(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	called := false
	job := extapi.SyncJob{
		Name:    "warehouse.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(ctx context.Context, req extapi.SyncJobContext) error {
			called = true
			return nil
		},
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].JobType != "integration.sync.acme.warehouse.stock" {
		t.Fatalf("SyncJobs() = %+v", jobs)
	}
	if called {
		t.Fatal("handler should not run at registration time")
	}
}

func TestApp_Integration_RegisterSyncJob_DuplicateRejected(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	job := extapi.SyncJob{Name: "pull", Trigger: extapi.OnEvent("order.created"), Handler: handler}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("first RegisterSyncJob: %v", err)
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err == nil {
		t.Fatal("expected duplicate sync job error")
	}
}

func TestApp_Integration_RegisterSyncJob_InvalidTrigger(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	err := app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "x",
		Trigger: extapi.SyncTrigger{Kind: "manual"},
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	if err == nil {
		t.Fatal("expected trigger error")
	}
}
