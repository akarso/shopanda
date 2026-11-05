package main

import (
	"database/sql"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// panickingMigrator exercises the wireServeRuntime migrate-failure path without DB.
type panickingMigrator struct{}

func (p *panickingMigrator) Name() string { return "shopanda/panic-mig" }
func (p *panickingMigrator) Init(*plugin.App) error {
	return nil
}
func (p *panickingMigrator) Migration() plugin.MigrationSpec {
	panic("migrations boom")
}

// Mirrors wireServeRuntime: seal after InitAll, then UnbindRuntime when
// InitSummaryError reports MigrateFailed (named-return defer in production).
func TestSealPermissionRegistry_UnbindsOnMigrateFailed(t *testing.T) {
	t.Cleanup(rbac.UnbindRuntime)

	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(&panickingMigrator{})
	app := &plugin.App{
		Logger:    log,
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	if summary.MigrateFailed != 1 {
		t.Fatalf("summary = %+v, want MigrateFailed=1", summary)
	}

	sealPermissionRegistry(app)
	if rbac.Runtime() == nil {
		t.Fatal("expected BindRuntime after seal")
	}

	err := plugin.InitSummaryError(summary)
	if err == nil || !strings.Contains(err.Error(), "migrate failed") {
		t.Fatalf("InitSummaryError = %v", err)
	}
	rbac.UnbindRuntime()
	if rbac.Runtime() != nil {
		t.Fatal("expected UnbindRuntime after migrate failure (wireServeRuntime path)")
	}
}
