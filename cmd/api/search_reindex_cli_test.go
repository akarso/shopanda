package main

import (
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// TestRunSearchReindex_UnknownArgument pins that an unrecognized argument
// is rejected before any DB connection is opened — the only part of
// runSearchReindex testable without a real Postgres instance (everything
// past arg parsing needs a DB, a plugin bootstrap, and the job queue).
func TestRunSearchReindex_UnknownArgument(t *testing.T) {
	err := runSearchReindex(io.Discard, &config.Config{}, logger.New("error"), []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("err = %v, want unknown argument error", err)
	}
}

// TestRunSearchReindex_WaitFlagAccepted pins that --wait is a recognized
// flag (parses past the arg loop) rather than being rejected outright.
// It still fails past that point (no reachable DB in this test), which is
// expected and not what this test is pinning.
func TestRunSearchReindex_WaitFlagAccepted(t *testing.T) {
	err := runSearchReindex(io.Discard, &config.Config{}, logger.New("error"), []string{"--wait"})
	if err != nil && strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("--wait was rejected as an unknown argument: %v", err)
	}
}
