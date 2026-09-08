package main

import (
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestParseReconcileArgs_RequiresRunID(t *testing.T) {
	if _, _, err := parseReconcileArgs([]string{"--reason=x"}); err == nil {
		t.Fatal("expected an error with no positional run-id")
	}
}

func TestParseReconcileArgs_RequiresReason(t *testing.T) {
	_, _, err := parseReconcileArgs([]string{"run-1"})
	if err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("err = %v, want a --reason required error", err)
	}
}

func TestParseReconcileArgs_RejectsUnknownFlag(t *testing.T) {
	_, _, err := parseReconcileArgs([]string{"run-1", "--reason=x", "--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("err = %v, want unknown argument error", err)
	}
}

func TestParseReconcileArgs_RejectsMultiplePositionals(t *testing.T) {
	if _, _, err := parseReconcileArgs([]string{"run-1", "run-2", "--reason=x"}); err == nil {
		t.Fatal("expected an error with two positional arguments")
	}
}

func TestParseReconcileArgs_ParsesRunIDAndReason(t *testing.T) {
	runID, reason, err := parseReconcileArgs([]string{"run-1", "--reason=worker confirmed dead"})
	if err != nil {
		t.Fatalf("parseReconcileArgs: %v", err)
	}
	if runID != "run-1" || reason != "worker confirmed dead" {
		t.Fatalf("got runID=%q reason=%q", runID, reason)
	}
}

// TestRunSearchReindexRunsReconcile_BadArgsFailsBeforeDB pins that
// argument validation happens before any DB connection is attempted — the
// only part of this command testable without a real Postgres instance.
func TestRunSearchReindexRunsReconcile_BadArgsFailsBeforeDB(t *testing.T) {
	err := runSearchReindexRunsReconcile(io.Discard, &config.Config{}, logger.New("error"), []string{"--reason=x"})
	if err == nil {
		t.Fatal("expected an error with no run-id argument")
	}
}
