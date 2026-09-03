package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// failingWriter always fails, for testing that a write error actually
// propagates instead of being discarded.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

func TestCliActor_NonEmpty(t *testing.T) {
	actor := cliActor()
	if actor == "" {
		t.Fatal("cliActor() = \"\", want a non-empty identifier")
	}
	if !strings.HasPrefix(actor, "cli") {
		t.Errorf("cliActor() = %q, want it prefixed with \"cli\" so it's never confused with a real admin user ID", actor)
	}
}

// TestParseSingleIDArg pins the fix for an unrecognized "--..." option
// silently being treated as the id/name instead of rejected — e.g.
// `app jobs:retry --tpyo` must not attempt to retry a job literally named
// "--tpyo".
func TestParseSingleIDArg(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantID  string
		wantErr bool
	}{
		{name: "valid id", args: []string{"job-1"}, wantID: "job-1"},
		{name: "no args", args: nil, wantErr: true},
		{name: "too many args", args: []string{"job-1", "job-2"}, wantErr: true},
		{name: "empty string", args: []string{""}, wantErr: true},
		{name: "whitespace only", args: []string{"   "}, wantErr: true},
		{name: "unrecognized long option", args: []string{"--json"}, wantErr: true},
		{name: "unrecognized option with value", args: []string{"--status=failed"}, wantErr: true},
		{name: "bare double dash", args: []string{"--"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := parseSingleIDArg("jobs:retry", tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSingleIDArg(%v) = (%q, nil), want an error", tc.args, id)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSingleIDArg(%v): %v", tc.args, err)
			}
			if id != tc.wantID {
				t.Errorf("id = %q, want %q", id, tc.wantID)
			}
		})
	}
}

// TestParseSingleIDArgs pins the same fix for the --json-accepting
// variant: --json must still work, but any other option token must be
// rejected rather than absorbed as the positional id/name.
func TestParseSingleIDArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantID     string
		wantJSON   bool
		wantErr    bool
		errMustHit string
	}{
		{name: "valid id no flags", args: []string{"job-1"}, wantID: "job-1"},
		{name: "valid id with --json", args: []string{"job-1", "--json"}, wantID: "job-1", wantJSON: true},
		{name: "--json before id", args: []string{"--json", "job-1"}, wantID: "job-1", wantJSON: true},
		{name: "no args", args: nil, wantErr: true},
		{name: "too many positionals", args: []string{"job-1", "job-2"}, wantErr: true},
		{name: "empty string", args: []string{""}, wantErr: true},
		{name: "unrecognized option", args: []string{"job-1", "--typo"}, wantErr: true, errMustHit: "--typo"},
		{name: "unrecognized option alone", args: []string{"--verbose"}, wantErr: true, errMustHit: "--verbose"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, jsonOut, err := parseSingleIDArgs("jobs:show", tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSingleIDArgs(%v) = (%q, %v, nil), want an error", tc.args, id, jsonOut)
				}
				if tc.errMustHit != "" && !strings.Contains(err.Error(), tc.errMustHit) {
					t.Fatalf("err = %v, want it to mention %q", err, tc.errMustHit)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSingleIDArgs(%v): %v", tc.args, err)
			}
			if id != tc.wantID || jsonOut != tc.wantJSON {
				t.Errorf("(id, jsonOut) = (%q, %v), want (%q, %v)", id, jsonOut, tc.wantID, tc.wantJSON)
			}
		})
	}
}

// TestFormatJobDetail_PropagatesWriteError pins the fix for formatJobDetail
// discarding fmt.Fprintf's error on every field line — a broken pipe
// partway through must surface as a returned error, not a nil (success).
func TestFormatJobDetail_PropagatesWriteError(t *testing.T) {
	job := &domainjobs.Detail{Summary: domainjobs.Summary{ID: "job-1"}}
	if err := formatJobDetail(failingWriter{}, job, false); err == nil {
		t.Fatal("expected the writer's error to propagate")
	}
}

// TestWriteSuccessLinef_PropagatesWriteError pins the same fix for the
// shared confirmation-line helper runJobsRetry/runJobsCancel/
// runScheduleSetEnabled/runScheduleTrigger all use for their final
// success message.
func TestWriteSuccessLinef_PropagatesWriteError(t *testing.T) {
	err := writeSuccessLinef(failingWriter{}, "Job %s queued for retry.\n", "job-1")
	if err == nil {
		t.Fatal("expected the writer's error to propagate")
	}
}

// TestAuditCLIAction_NilConnDoesNotPanic pins that a failure to construct
// the audit repository (e.g. a nil/broken connection) is handled
// gracefully — logged, not panicked — matching auditCLIAction's
// documented best-effort contract: a missing audit row must not crash the
// CLI command that triggered it.
func TestAuditCLIAction_NilConnDoesNotPanic(t *testing.T) {
	auditCLIAction(context.Background(), nil, logger.New("error"), adminApp.AuditJobRetry, "job", "job-1", nil)
}
