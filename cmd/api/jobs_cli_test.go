package main

import (
	"bytes"
	"io"
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

// TestNewJobsService_NonPostgresDriverFailsFast pins the fix for jobs:*
// commands silently querying an unrelated Postgres jobs table when a
// broker queue driver (redis/rabbitmq/kafka/sqs) is actually configured:
// newJobsService must fail clearly, before even opening a DB connection,
// rather than construct a Postgres queue nothing else is using.
func TestNewJobsService_NonPostgresDriverFailsFast(t *testing.T) {
	cfg := &config.Config{}
	cfg.Queue.Driver = "redis"

	_, _, err := newJobsService(cfg)
	if err == nil {
		t.Fatal("expected an error for a non-postgres queue driver")
	}
	if !strings.Contains(err.Error(), "queue.driver=postgres") {
		t.Fatalf("err = %v, want it to name the postgres-only requirement", err)
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
