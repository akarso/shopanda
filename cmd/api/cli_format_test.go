package main

import (
	"context"
	"strings"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestCliActor_NonEmpty(t *testing.T) {
	actor := cliActor()
	if actor == "" {
		t.Fatal("cliActor() = \"\", want a non-empty identifier")
	}
	if !strings.HasPrefix(actor, "cli") {
		t.Errorf("cliActor() = %q, want it prefixed with \"cli\" so it's never confused with a real admin user ID", actor)
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
