package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// These tests cover argument parsing/usage-error paths and the pure
// format* functions, which don't need a database. runJobsList/Show/Retry/
// Cancel's actual DB-backed behavior (a real Postgres-backed jobs table)
// is exercised by internal/interfaces/http/admin/job_admin_test.go against
// the same jobsApp.Service — this file doesn't re-test that, only the
// CLI-specific plumbing (arg parsing, output formatting).

// TestNewJobsIntrospection_NonPostgresDriverFailsFast pins the fix for jobs:*
// commands silently querying an unrelated Postgres jobs table when a
// broker queue driver (redis/rabbitmq/kafka/sqs) is actually configured:
// newJobsIntrospection must fail clearly, before even opening a DB connection,
// rather than construct a Postgres queue nothing else is using.
func TestNewJobsIntrospection_NonPostgresDriverFailsFast(t *testing.T) {
	cfg := &config.Config{}
	cfg.Queue.Driver = "redis"

	_, _, err := newJobsIntrospection(cfg)
	if err == nil {
		t.Fatal("expected an error for a non-postgres queue driver")
	}
	if !strings.Contains(err.Error(), "queue.driver=postgres") {
		t.Fatalf("err = %v, want it to name the postgres-only requirement", err)
	}
}

func TestNewJobsRuntime_NonPostgresDriverFailsFast(t *testing.T) {
	cfg := &config.Config{}
	cfg.Queue.Driver = "redis"
	_, err := newJobsRuntime(cfg, logger.New("error"))
	if err == nil {
		t.Fatal("expected an error for a non-postgres queue driver")
	}
	if !strings.Contains(err.Error(), "queue.driver=postgres") {
		t.Fatalf("err = %v, want it to name the postgres-only requirement", err)
	}
}

// TestNewJobsRuntime_WarehousedemoReachesBind pins CapIntegrationStock
// wiring before InitAll — without wireIntegrationStockSyncerFromDB,
// warehousedemo Init fails and jobs:retry|cancel|requeue abort.
func TestNewJobsRuntime_WarehousedemoReachesBind(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	t.Setenv("DATABASE_URL", dsn)
	// Empty media.storage still loads core/storage-local; give it a usable
	// local root so Init does not fail on empty base_path (and hide under
	// io.Discard). Dev mode allows empty sign_secret (ephemeral key).
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	cfg := &config.Config{}
	cfg.Queue.Driver = "postgres"
	cfg.Media.Local.BasePath = t.TempDir()
	cfg.Media.Local.BaseURL = "http://127.0.0.1/media"
	cfg.Plugins.WarehouseDemo.Enabled = true
	cfg.Plugins.WarehouseDemo.WarehouseBaseURL = "http://127.0.0.1:9/warehouse"
	var logBuf bytes.Buffer
	rt, err := newJobsRuntime(cfg, logger.NewWithWriter(&logBuf, "error"))
	if err != nil {
		t.Fatalf("newJobsRuntime with warehousedemo: %v\ninit logs:\n%s", err, logBuf.String())
	}
	defer rt.conn.Close()
	if rt.queue == nil || rt.svc == nil {
		t.Fatal("expected bound queue and jobs service")
	}
}

func TestRunJobsList_UnknownArgument(t *testing.T) {
	err := runJobsList(io.Discard, &config.Config{}, logger.New("error"), []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("err = %v, want unknown argument error", err)
	}
}

func TestRunJobsShow_RequiresID(t *testing.T) {
	err := runJobsShow(io.Discard, &config.Config{}, logger.New("error"), nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestRunJobsRetry_RequiresID(t *testing.T) {
	err := runJobsRetry(io.Discard, &config.Config{}, logger.New("error"), nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestRunJobsRetry_TooManyArgs(t *testing.T) {
	err := runJobsRetry(io.Discard, &config.Config{}, logger.New("error"), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestRunJobsList_InvalidStatus(t *testing.T) {
	err := runJobsList(io.Discard, &config.Config{}, logger.New("error"), []string{"--status=faild"})
	if err == nil || !strings.Contains(err.Error(), "invalid --status") {
		t.Fatalf("err = %v, want invalid --status error for a typo'd value", err)
	}
}

func TestRunJobsList_InvalidLimit(t *testing.T) {
	err := runJobsList(io.Discard, &config.Config{}, logger.New("error"), []string{"--limit=0"})
	if err == nil || !strings.Contains(err.Error(), "--limit must be a positive integer") {
		t.Fatalf("err = %v, want --limit validation error", err)
	}
}

func TestRunJobsList_InvalidOffset(t *testing.T) {
	err := runJobsList(io.Discard, &config.Config{}, logger.New("error"), []string{"--offset=-1"})
	if err == nil || !strings.Contains(err.Error(), "--offset must be a non-negative integer") {
		t.Fatalf("err = %v, want --offset validation error", err)
	}
}

func TestRunJobsCancel_RequiresID(t *testing.T) {
	err := runJobsCancel(io.Discard, &config.Config{}, logger.New("error"), nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestParseJobsRequeueArgs(t *testing.T) {
	got, err := parseJobsRequeueArgs([]string{"job-1"})
	if err != nil || got.ID != "job-1" || got.MinAge != domainjobs.DefaultRequeueMinAge || got.Terminal || got.Force {
		t.Fatalf("default: %+v err=%v", got, err)
	}
	got, err = parseJobsRequeueArgs([]string{"job-2", "--min-age=90s"})
	if err != nil || got.ID != "job-2" || got.MinAge != 90*time.Second || got.Terminal {
		t.Fatalf("flag: %+v err=%v", got, err)
	}
	got, err = parseJobsRequeueArgs([]string{"job-3", "--terminal"})
	if err != nil || got.ID != "job-3" || !got.Terminal || got.Force {
		t.Fatalf("terminal: %+v err=%v", got, err)
	}
	got, err = parseJobsRequeueArgs([]string{"job-4", "--terminal", "--force"})
	if err != nil || !got.Terminal || !got.Force {
		t.Fatalf("force: %+v err=%v", got, err)
	}
	if _, err := parseJobsRequeueArgs([]string{"job-5", "--force"}); err == nil {
		t.Fatal("expected --force without --terminal error")
	}
	if _, err := parseJobsRequeueArgs([]string{"job-6", "--terminal", "--min-age=90s"}); err == nil {
		t.Fatal("expected error combining --terminal and --min-age")
	}
	if _, err := parseJobsRequeueArgs([]string{"--min-age=1ns", "x"}); err == nil {
		t.Fatal("expected invalid min-age below 1s")
	}
	if _, err := parseJobsRequeueArgs(nil); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestValidateJobsRequeueStatus(t *testing.T) {
	if err := validateJobsRequeueStatus(domainjobs.StatusFailed, 1, 5, true, false); err != nil {
		t.Fatalf("failed+terminal: %v", err)
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusCancelled, 1, 5, true, false); err == nil {
		t.Fatal("cancelled without --force must fail")
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusDone, 1, 5, true, false); err == nil {
		t.Fatal("done without --force must fail")
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusDone, 1, 5, true, true); err != nil {
		t.Fatalf("done+force: %v", err)
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusCancelled, 1, 5, true, true); err != nil {
		t.Fatalf("cancelled+force: %v", err)
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusProcessing, 1, 5, true, false); err == nil {
		t.Fatal("processing+terminal must fail")
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusPending, 0, 5, true, false); err == nil {
		t.Fatal("pending+terminal must fail")
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusFailed, 3, 3, true, false); err == nil || !strings.Contains(err.Error(), "jobs:retry") {
		t.Fatalf("exhausted without force: %v", err)
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusFailed, 3, 3, true, true); err != nil {
		t.Fatalf("exhausted+force: %v", err)
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusFailed, 1, 5, true, true); err == nil {
		t.Fatal("force on non-exhausted failed must fail")
	}
	if err := validateJobsRequeueStatus(domainjobs.StatusProcessing, 1, 5, false, false); err != nil {
		t.Fatalf("processing: %v", err)
	}
}

func TestJobsRequeueSuccessMessage_ExhaustedWarns(t *testing.T) {
	msg := jobsRequeueSuccessMessage("j1", domainjobs.StatusFailed, 3, 3, true, 0)
	if !strings.Contains(msg, "attempts>=max_retries") || !strings.Contains(msg, "jobs:retry") {
		t.Fatalf("msg=%q, want exhausted warning + jobs:retry", msg)
	}
	msg = jobsRequeueSuccessMessage("j1", domainjobs.StatusDone, 1, 5, true, 0)
	if !strings.Contains(msg, "resurrected") {
		t.Fatalf("msg=%q, want done resurrection warning", msg)
	}
}

func TestFormatJobsRequeueError_Mapping(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{domainjobs.ErrRequeueNotFound, "disappeared"},
		{domainjobs.ErrRequeueConflict, "reclaim race"},
		{domainjobs.ErrRequeueNotProcessing, "no longer processing"},
		{domainjobs.ErrRequeueNotTerminal, "no longer terminal"},
		{domainjobs.ErrRequeueWrongAttempt, "TOCTOU"},
	}
	for _, tc := range cases {
		got := formatJobsRequeueError(tc.err, domainjobs.DefaultRequeueMinAge)
		if got == nil || !errors.Is(got, tc.err) || !strings.Contains(got.Error(), tc.want) {
			t.Fatalf("err=%v got=%v want substring %q", tc.err, got, tc.want)
		}
	}
}

func TestEffectiveJobsRequeueForce(t *testing.T) {
	if effectiveJobsRequeueForce(domainjobs.StatusFailed, 1, 5, true) {
		t.Fatal("force on non-exhausted failed must audit false")
	}
	if !effectiveJobsRequeueForce(domainjobs.StatusDone, 1, 5, true) {
		t.Fatal("force on done must audit true")
	}
	if !effectiveJobsRequeueForce(domainjobs.StatusFailed, 3, 3, true) {
		t.Fatal("force on exhausted failed must audit true")
	}
}

func TestFormatJobsList_TextOutput(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jobList := []domainjobs.Summary{
		{ID: "job-1", Type: "email.send", Status: domainjobs.StatusFailed, Attempts: 3, MaxRetries: 5, RunAt: now},
	}
	var buf bytes.Buffer
	if err := formatJobsList(&buf, jobList, domainjobs.ListFilter{Limit: 20}, false); err != nil {
		t.Fatalf("formatJobsList: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ID", "TYPE", "STATUS", "job-1", "email.send", "failed", "3/5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatJobsList_EmptyText(t *testing.T) {
	var buf bytes.Buffer
	if err := formatJobsList(&buf, nil, domainjobs.ListFilter{Limit: 20}, false); err != nil {
		t.Fatalf("formatJobsList: %v", err)
	}
	if !strings.Contains(buf.String(), "No jobs found.") {
		t.Fatalf("output = %q, want a no-jobs message", buf.String())
	}
}

func TestFormatJobsList_JSONOutput(t *testing.T) {
	jobList := []domainjobs.Summary{{ID: "job-1", Type: "email.send"}}
	var buf bytes.Buffer
	if err := formatJobsList(&buf, jobList, domainjobs.ListFilter{Limit: 20}, true); err != nil {
		t.Fatalf("formatJobsList: %v", err)
	}
	if !strings.Contains(buf.String(), `"id": "job-1"`) {
		t.Fatalf("json output = %s", buf.String())
	}
}

// TestFormatJobsList_TruncationHint pins the fix for jobs:list silently
// truncating a large result set with no indication more rows exist: a
// full page (len(jobList) == filter.Limit) must print a hint naming the
// --offset to see the next page.
func TestFormatJobsList_TruncationHint(t *testing.T) {
	jobList := []domainjobs.Summary{{ID: "job-1"}, {ID: "job-2"}}
	var buf bytes.Buffer
	if err := formatJobsList(&buf, jobList, domainjobs.ListFilter{Limit: 2, Offset: 10}, false); err != nil {
		t.Fatalf("formatJobsList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "showing 2 job(s)") || !strings.Contains(out, "--offset=12") {
		t.Fatalf("output missing truncation hint:\n%s", out)
	}
}

// TestFormatJobsList_NoHintWhenNotFull confirms the hint only appears when
// the page is actually full — a short page unambiguously means there's
// nothing more, and printing a hint there would be misleading.
func TestFormatJobsList_NoHintWhenNotFull(t *testing.T) {
	jobList := []domainjobs.Summary{{ID: "job-1"}}
	var buf bytes.Buffer
	if err := formatJobsList(&buf, jobList, domainjobs.ListFilter{Limit: 20}, false); err != nil {
		t.Fatalf("formatJobsList: %v", err)
	}
	if strings.Contains(buf.String(), "there may be more") {
		t.Fatalf("output = %q, want no truncation hint for a short page", buf.String())
	}
}

func TestFormatJobDetail_TextOutput(t *testing.T) {
	job := &domainjobs.Detail{
		Summary: domainjobs.Summary{
			ID: "job-1", Type: "email.send", Status: domainjobs.StatusFailed,
			Attempts: 2, MaxRetries: 5,
		},
		Payload:   map[string]interface{}{"to": "a@example.com"},
		LastError: "smtp timeout",
	}
	var buf bytes.Buffer
	if err := formatJobDetail(&buf, job, false); err != nil {
		t.Fatalf("formatJobDetail: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"job-1", "email.send", "failed", "2/5", "smtp timeout", "a@example.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatJobDetail_JSONOutput(t *testing.T) {
	job := &domainjobs.Detail{Summary: domainjobs.Summary{ID: "job-1"}}
	var buf bytes.Buffer
	if err := formatJobDetail(&buf, job, true); err != nil {
		t.Fatalf("formatJobDetail: %v", err)
	}
	if !strings.Contains(buf.String(), `"id": "job-1"`) {
		t.Fatalf("json output = %s", buf.String())
	}
}
