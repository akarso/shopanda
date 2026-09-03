package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Like jobs_cli_test.go: covers argument parsing/usage-error paths and the
// pure format* functions, which don't need a database. The DB-backed
// behavior of List/Enable/Disable is exercised by
// internal/interfaces/http/admin/schedule_admin_test.go against the same
// schedulerApp.Service; runScheduleTrigger's local-scheduler-registration
// path has no analog there and would need a real Postgres-backed dev
// instance to exercise end to end (see PR-1032.md's Validation section).

func TestRunScheduleList_UnknownArgument(t *testing.T) {
	err := runScheduleList(io.Discard, &config.Config{}, logger.New("error"), []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("err = %v, want unknown argument error", err)
	}
}

func TestRunScheduleEnable_RequiresName(t *testing.T) {
	err := runScheduleEnable(io.Discard, &config.Config{}, logger.New("error"), nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestRunScheduleDisable_RequiresName(t *testing.T) {
	err := runScheduleDisable(io.Discard, &config.Config{}, logger.New("error"), nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestRunScheduleTrigger_RequiresName(t *testing.T) {
	err := runScheduleTrigger(io.Discard, &config.Config{}, logger.New("error"), nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestFormatSchedulesList_TextOutput(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	entries := []domainscheduler.CatalogEntry{
		{Name: "cache.cleanup", Spec: "*/5 * * * *", NextRun: now, Enabled: true},
	}
	var buf bytes.Buffer
	if err := formatSchedulesList(&buf, entries, false); err != nil {
		t.Fatalf("formatSchedulesList: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"NAME", "SPEC", "cache.cleanup", "*/5 * * * *", "true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatSchedulesList_EmptyText(t *testing.T) {
	var buf bytes.Buffer
	if err := formatSchedulesList(&buf, nil, false); err != nil {
		t.Fatalf("formatSchedulesList: %v", err)
	}
	if !strings.Contains(buf.String(), "No scheduled tasks registered.") {
		t.Fatalf("output = %q, want a no-schedules message", buf.String())
	}
}

func TestFormatSchedulesList_JSONOutput(t *testing.T) {
	entries := []domainscheduler.CatalogEntry{{Name: "cache.cleanup", Spec: "*/5 * * * *", Enabled: true}}
	var buf bytes.Buffer
	if err := formatSchedulesList(&buf, entries, true); err != nil {
		t.Fatalf("formatSchedulesList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"name": "cache.cleanup"`) {
		t.Fatalf("json output = %s", out)
	}
	// A zero NextRun must serialize as "" (matching schedule_admin.go's
	// toScheduleResponses), not the table view's "-" placeholder.
	if !strings.Contains(out, `"next_run": ""`) {
		t.Fatalf("json output = %s, want next_run: \"\" for a zero NextRun", out)
	}
}
