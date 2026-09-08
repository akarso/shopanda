package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// runSearchReindexRunsReconcile handles
// `app search:reindex-runs:reconcile <run-id> --reason=<text>` — the
// manual counterpart to the automatic search.reindex.reconcile sweep
// (internal/application/search.ReconcileHandler), for a run stuck
// "processing" whose underlying job the sweep can't safely judge on its
// own (still shows pending/processing, or is missing outright) — see
// RUNBOOK.md's "Search reindex" section. An operator who has independently
// confirmed the job is never coming back uses this instead of editing
// search_index_runs by hand.
func runSearchReindexRunsReconcile(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	runID, reason, err := parseReconcileArgs(args)
	if err != nil {
		return err
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	runRepo, err := postgres.NewSearchIndexRunRepo(conn)
	if err != nil {
		return fmt.Errorf("search:reindex-runs:reconcile: %w", err)
	}

	ctx := context.Background()
	reconcileErr := searchApp.ReconcileRunManually(ctx, runRepo, runID, reason)
	auditCLIAction(ctx, conn, log, adminApp.AuditSearchReindexRunReconcile, "search_index_run", runID, reconcileErr)
	if reconcileErr != nil {
		var appErr *apperror.Error
		if errors.As(reconcileErr, &appErr) {
			return fmt.Errorf("search:reindex-runs:reconcile: %s", appErr.Message)
		}
		return fmt.Errorf("search:reindex-runs:reconcile: %w", reconcileErr)
	}
	return writeSuccessLinef(w, "Reindex run %s marked failed.\n", runID)
}

// parseReconcileArgs requires exactly one positional <run-id> plus a
// mandatory --reason=<text> flag — unlike jobs:retry/jobs:cancel, this
// mutation permanently overwrites a run's outcome with an operator
// judgment call, so the reason isn't optional the way it would be for a
// simple retry.
func parseReconcileArgs(args []string) (runID, reason string, err error) {
	const usage = "usage: app search:reindex-runs:reconcile <run-id> --reason=<text>"
	var positional []string
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--reason="):
			reason = strings.TrimPrefix(arg, "--reason=")
		case strings.HasPrefix(arg, "--"):
			return "", "", fmt.Errorf("search:reindex-runs:reconcile: unknown argument %q (%s)", arg, usage)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return "", "", fmt.Errorf(usage)
	}
	if strings.TrimSpace(reason) == "" {
		return "", "", fmt.Errorf("search:reindex-runs:reconcile: --reason=<text> is required (%s)", usage)
	}
	return positional[0], reason, nil
}
