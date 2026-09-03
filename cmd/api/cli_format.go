package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os/user"
	"strings"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// errWriter accumulates the first write error across a sequence of
// Fprintf calls, so a multi-line command output (e.g. formatJobDetail's
// field-by-field dump) can check one error at the end instead of after
// every single fmt.Fprintf — while still actually checking it, unlike
// discarding each call's return value. A stdout write essentially never
// fails in normal use, but a broken pipe (e.g. piping into `head`) is a
// real, if rare, case where it does — and a CLI command whose output
// silently stopped partway through should not still exit 0. Once an
// error occurs, subsequent printf calls become no-ops (matches the
// standard "errWriter" idiom for this exact situation).
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) printf(format string, args ...interface{}) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, args...)
}

// writeSuccessLinef writes a single confirmation line (the "Job X queued
// for retry."/"Schedule Y enabled."/... messages every mutating command
// prints on success) and returns any write error, instead of discarding
// it — a shared, directly-testable primitive so runJobsRetry/
// runJobsCancel/runScheduleSetEnabled/runScheduleTrigger don't each
// duplicate (and each risk re-discarding) the same one-line check.
func writeSuccessLinef(w io.Writer, format string, args ...interface{}) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

// cliActor identifies who ran a CLI-initiated admin action, for the audit
// log's AdminID field. There's no authenticated admin session in a CLI
// invocation (unlike the HTTP admin API, which always has one by the time
// a handler runs — RequirePermission enforces it) — this uses the OS user
// running the command instead, prefixed so it's never confused with a
// real admin user ID in the audit log.
func cliActor() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return "cli"
	}
	return "cli:" + u.Username
}

// auditCLIAction writes an admin audit log entry for a CLI-initiated
// mutating command (retry/cancel/enable/disable/trigger), reusing the
// command's already-open DB connection — matching the "every mutating
// admin action is audited" guarantee RUNBOOK.md documents for the HTTP
// admin API, which the original CLI implementation silently didn't honor.
// A failure to write the audit entry is logged but never fails the CLI
// command itself — same best-effort persistence contract
// Auditor.LogAction already has for the HTTP path (a missing audit row
// must not be how an operator finds out their retry/cancel/trigger
// didn't go through).
func auditCLIAction(ctx context.Context, conn *sql.DB, log logger.Logger, action adminApp.AuditAction, resourceType, resourceID string, cmdErr error) {
	repo, err := postgres.NewAuditLogRepo(conn)
	if err != nil {
		log.Error("cli.audit.repo_failed", err, map[string]interface{}{"action": string(action)})
		return
	}
	auditor := adminApp.NewAuditor(log)
	auditor.SetAuditLogRepository(repo)

	result := "success"
	errMsg := ""
	if cmdErr != nil {
		result = "error"
		errMsg = cmdErr.Error()
	}
	auditor.LogAction(ctx, adminApp.AuditEntry{
		AdminID:      cliActor(),
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Result:       result,
		Error:        errMsg,
	})
}

// writeJSON marshals v as indented JSON to w, matching
// pluginreport.FormatJSON's convention (the one existing --json precedent
// in this codebase).
func writeJSON(w io.Writer, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		_, err = w.Write([]byte("\n"))
	}
	return err
}

// formatCLITimestamp renders a time.Time as RFC3339, or "-" for the zero
// value (e.g. a schedule catalog entry whose NextRun couldn't be computed)
// — for human-readable table/text output.
func formatCLITimestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// formatCLITimestampJSON renders a time.Time as RFC3339, or "" for the
// zero value. This matches schedule_admin.go's toScheduleResponses
// exactly (NextRun can legitimately be zero when cron.NextRun can't
// compute one). It does NOT match job_admin.go's toJobSummaryResponses/
// toJobDetailResponse, which never check IsZero() and would instead
// render a zero time as "0001-01-01T00:00:00Z" — harmless in practice
// (a job row's timestamp columns are always populated by Postgres, so a
// zero time.Time never actually reaches that code), but a real
// divergence from what this function does, not something to rely on for
// jobs specifically.
func formatCLITimestampJSON(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// parseSingleIDArg requires exactly one positional argument (an id/name),
// no flags — used by action commands (retry/cancel/enable/disable/trigger)
// that take no other options. Rejects anything that looks like an option
// (starts with "--") rather than silently treating it as the id/name —
// matching --status='s own typo-fails-loudly precedent, so `app
// jobs:retry --tpyo` doesn't attempt to retry a job literally named
// "--tpyo".
func parseSingleIDArg(command string, args []string) (string, error) {
	if len(args) != 1 || strings.HasPrefix(args[0], "--") || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("usage: app %s <id>", command)
	}
	return args[0], nil
}

// parseSingleIDArgs requires one positional argument plus an optional
// --json flag — used by show-style commands. Any other "--"-prefixed
// token is rejected as an unrecognized option instead of silently being
// treated as the positional id/name (see parseSingleIDArg's comment).
func parseSingleIDArgs(command string, args []string) (id string, jsonOut bool, err error) {
	var positional []string
	for _, arg := range args {
		switch {
		case arg == "--json":
			jsonOut = true
		case strings.HasPrefix(arg, "--"):
			return "", false, fmt.Errorf("usage: app %s <id> [--json]: unrecognized option %q", command, arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return "", false, fmt.Errorf("usage: app %s <id> [--json]", command)
	}
	return positional[0], jsonOut, nil
}

// columnWriter is a minimal hand-formatted table writer. No table
// library exists anywhere in this codebase (no tabwriter, no third-party
// package) — internal/application/pluginreport's manual
// fmt.Fprintf-with-%-*s-padding is the closest existing convention
// (`migrate`, the command the PR spec pointed at, produces no table
// output at all — see PR-1032.md), and this mirrors that style rather
// than introducing a new one.
type columnWriter struct {
	w      io.Writer
	widths []int
	header []string
	rows   [][]string
}

func newColumnWriter(w io.Writer, header ...string) *columnWriter {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	return &columnWriter{w: w, widths: widths, header: header}
}

func (t *columnWriter) row(cols ...string) {
	for i, c := range cols {
		if i < len(t.widths) && len(c) > t.widths[i] {
			t.widths[i] = len(c)
		}
	}
	t.rows = append(t.rows, cols)
}

func (t *columnWriter) flush() error {
	if err := t.writeRow(t.header); err != nil {
		return err
	}
	for _, r := range t.rows {
		if err := t.writeRow(r); err != nil {
			return err
		}
	}
	return nil
}

func (t *columnWriter) writeRow(cols []string) error {
	var b strings.Builder
	for i, c := range cols {
		if i > 0 {
			b.WriteString("  ")
		}
		if i < len(t.widths)-1 {
			fmt.Fprintf(&b, "%-*s", t.widths[i], c)
		} else {
			b.WriteString(c)
		}
	}
	b.WriteString("\n")
	_, err := t.w.Write([]byte(b.String()))
	return err
}
