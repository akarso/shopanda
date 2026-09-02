package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	jobsApp "github.com/akarso/shopanda/internal/application/jobs"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// jobsListDefaultLimit/jobsListMaxLimit mirror jobsApp.Service's own
// unexported defaultListLimit/maxListLimit constants — duplicated
// deliberately (small, stable values, not complex logic) so this CLI can
// clamp --limit client-side and know, afterward, exactly what limit the
// service actually applied, without relying on Service.List mutating a
// filter its caller passed by value (it doesn't — Go value semantics).
// That's what makes the "showing N, more may exist" truncation hint in
// formatJobsList reliable.
const (
	jobsListDefaultLimit = 20
	jobsListMaxLimit     = 100
)

var validJobStatuses = map[domainjobs.Status]bool{
	domainjobs.StatusPending:    true,
	domainjobs.StatusProcessing: true,
	domainjobs.StatusDone:       true,
	domainjobs.StatusFailed:     true,
	domainjobs.StatusCancelled:  true,
}

func validJobStatusesList() string {
	return "pending, processing, done, failed, cancelled"
}

// newJobsService opens a standalone DB connection and constructs the same
// jobsApp.Service the HTTP admin API (job_admin.go) calls — CLI and API
// never diverge in retry/cancel/list decision logic. Postgres-queue-only,
// matching runScheduler's existing standalone construction (postgres.NewJobQueue
// directly, not the plugin-override-aware resolveJobQueue used by
// wireServeRuntime/setupWorker) — a CLI invocation is a short-lived,
// single-purpose process, not the long-running server that needs to
// respect a configured broker-backed queue plugin. Returns the open conn
// too (not just a closer) so mutating commands can build an audit-log
// repository off the same connection.
func newJobsService(cfg *config.Config) (svc *jobsApp.Service, conn *sql.DB, err error) {
	dsn := config.DatabaseDSN(cfg)
	conn, err = db.Open(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w", err)
	}
	jobQueue, err := postgres.NewJobQueue(conn)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("job queue: %w", err)
	}
	svc, err = jobsApp.NewService(jobQueue, jobQueue)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("jobs service: %w", err)
	}
	return svc, conn, nil
}

// runJobsList handles `app jobs:list [--type=X] [--status=Y] [--limit=N] [--offset=N] [--json]`.
func runJobsList(w io.Writer, cfg *config.Config, _ logger.Logger, args []string) error {
	filter := domainjobs.ListFilter{Limit: jobsListDefaultLimit}
	jsonOut := false
	for _, arg := range args {
		switch {
		case arg == "--json":
			jsonOut = true
		case strings.HasPrefix(arg, "--type="):
			filter.Type = strings.TrimPrefix(arg, "--type=")
		case strings.HasPrefix(arg, "--status="):
			status := domainjobs.Status(strings.TrimPrefix(arg, "--status="))
			if !validJobStatuses[status] {
				return fmt.Errorf("jobs:list: invalid --status=%q (valid: %s)", status, validJobStatusesList())
			}
			filter.Status = status
		case strings.HasPrefix(arg, "--limit="):
			n, convErr := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if convErr != nil || n < 1 {
				return fmt.Errorf("jobs:list: --limit must be a positive integer")
			}
			filter.Limit = n
		case strings.HasPrefix(arg, "--offset="):
			n, convErr := strconv.Atoi(strings.TrimPrefix(arg, "--offset="))
			if convErr != nil || n < 0 {
				return fmt.Errorf("jobs:list: --offset must be a non-negative integer")
			}
			filter.Offset = n
		default:
			return fmt.Errorf("jobs:list: unknown argument %q (usage: jobs:list [--type=X] [--status=Y] [--limit=N] [--offset=N] [--json])", arg)
		}
	}
	if filter.Limit > jobsListMaxLimit {
		filter.Limit = jobsListMaxLimit
	}

	svc, conn, err := newJobsService(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	jobList, err := svc.List(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("jobs:list: %w", err)
	}
	return formatJobsList(w, jobList, filter, jsonOut)
}

func formatJobsList(w io.Writer, jobList []domainjobs.Summary, filter domainjobs.ListFilter, jsonOut bool) error {
	if jsonOut {
		out := make([]map[string]interface{}, len(jobList))
		for i, j := range jobList {
			out[i] = jobSummaryToJSON(j)
		}
		return writeJSON(w, out)
	}
	if len(jobList) == 0 {
		_, err := fmt.Fprintln(w, "No jobs found.")
		return err
	}
	tw := newColumnWriter(w, "ID", "TYPE", "STATUS", "ATTEMPTS", "RUN AT")
	for _, j := range jobList {
		tw.row(j.ID, j.Type, string(j.Status), fmt.Sprintf("%d/%d", j.Attempts, j.MaxRetries), formatCLITimestamp(j.RunAt))
	}
	if err := tw.flush(); err != nil {
		return err
	}
	// filter.Limit was clamped client-side to jobsListMaxLimit before the
	// List call, matching what jobsApp.Service would clamp it to
	// internally — so a full page here reliably means there may be more
	// rows past it, not just a coincidental exact match.
	if len(jobList) == filter.Limit {
		_, err := fmt.Fprintf(w, "\n(showing %d job(s) — there may be more; re-run with --offset=%d to see the next page)\n", len(jobList), filter.Offset+filter.Limit)
		return err
	}
	return nil
}

// jobSummaryToJSON mirrors internal/interfaces/http/admin/job_admin.go's
// toJobSummaryResponses field-for-field (snake_case keys) — kept as a
// separate, duplicated mapping rather than importing that unexported HTTP
// package function, so a script can reuse the same jq filters against
// `jobs:list --json` and `GET /admin/jobs` without the CLI depending on
// the HTTP interface adapter (each interface owns its own presentation).
func jobSummaryToJSON(j domainjobs.Summary) map[string]interface{} {
	return map[string]interface{}{
		"id":          j.ID,
		"type":        j.Type,
		"status":      string(j.Status),
		"attempts":    j.Attempts,
		"max_retries": j.MaxRetries,
		"run_at":      formatCLITimestampJSON(j.RunAt),
		"created_at":  formatCLITimestampJSON(j.CreatedAt),
		"updated_at":  formatCLITimestampJSON(j.UpdatedAt),
	}
}

// runJobsShow handles `app jobs:show <id> [--json]`.
func runJobsShow(w io.Writer, cfg *config.Config, _ logger.Logger, args []string) error {
	id, jsonOut, err := parseSingleIDArgs("jobs:show", args)
	if err != nil {
		return err
	}

	svc, conn, err := newJobsService(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	job, err := svc.Get(context.Background(), id)
	if err != nil {
		return fmt.Errorf("jobs:show: %w", err)
	}
	if job == nil {
		return fmt.Errorf("jobs:show: no job with id %q", id)
	}
	return formatJobDetail(w, job, jsonOut)
}

func formatJobDetail(w io.Writer, job *domainjobs.Detail, jsonOut bool) error {
	if jsonOut {
		out := jobSummaryToJSON(job.Summary)
		out["payload"] = job.Payload
		out["last_error"] = job.LastError
		return writeJSON(w, out)
	}
	fmt.Fprintf(w, "ID:          %s\n", job.ID)
	fmt.Fprintf(w, "Type:        %s\n", job.Type)
	fmt.Fprintf(w, "Status:      %s\n", job.Status)
	fmt.Fprintf(w, "Attempts:    %d/%d\n", job.Attempts, job.MaxRetries)
	fmt.Fprintf(w, "Run at:      %s\n", formatCLITimestamp(job.RunAt))
	fmt.Fprintf(w, "Created at:  %s\n", formatCLITimestamp(job.CreatedAt))
	fmt.Fprintf(w, "Updated at:  %s\n", formatCLITimestamp(job.UpdatedAt))
	if job.LastError != "" {
		fmt.Fprintf(w, "Last error:  %s\n", job.LastError)
	}
	payload, err := json.MarshalIndent(job.Payload, "", "  ")
	if err != nil {
		return fmt.Errorf("jobs:show: marshal payload: %w", err)
	}
	fmt.Fprintf(w, "Payload:\n%s\n", payload)
	return nil
}

// runJobsRetry handles `app jobs:retry <id>`.
func runJobsRetry(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	id, err := parseSingleIDArg("jobs:retry", args)
	if err != nil {
		return err
	}

	svc, conn, err := newJobsService(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	retryErr := svc.Retry(ctx, id)
	auditCLIAction(ctx, conn, log, adminApp.AuditJobRetry, "job", id, retryErr)
	if retryErr != nil {
		return fmt.Errorf("jobs:retry: %w", retryErr)
	}
	fmt.Fprintf(w, "Job %s queued for retry.\n", id)
	return nil
}

// runJobsCancel handles `app jobs:cancel <id>`.
func runJobsCancel(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	id, err := parseSingleIDArg("jobs:cancel", args)
	if err != nil {
		return err
	}

	svc, conn, err := newJobsService(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	cancelErr := svc.Cancel(ctx, id)
	auditCLIAction(ctx, conn, log, adminApp.AuditJobCancel, "job", id, cancelErr)
	if cancelErr != nil {
		return fmt.Errorf("jobs:cancel: %w", cancelErr)
	}
	fmt.Fprintf(w, "Job %s cancelled.\n", id)
	return nil
}
