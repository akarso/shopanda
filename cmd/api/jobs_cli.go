package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	jobsApp "github.com/akarso/shopanda/internal/application/jobs"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
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

// jobsCLIRuntime is the bound queue (OnQueueBound-wrapped) used by
// retry/cancel/requeue so mutate paths share one fenced instance.
type jobsCLIRuntime struct {
	svc   *jobsApp.Service
	conn  *sql.DB
	queue domainjobs.Queue
}

// newJobsIntrospection opens postgres Reader/Admin only — no plugin init.
// Used by list/show so read paths stay cheap.
func newJobsIntrospection(cfg *config.Config) (svc *jobsApp.Service, conn *sql.DB, err error) {
	if cfg.Queue.Driver != "postgres" {
		return nil, nil, fmt.Errorf("jobs:* commands require queue.driver=postgres — job introspection has no equivalent for %q (Postgres-queue-only; see internal/domain/jobs/reader.go)", cfg.Queue.Driver)
	}
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

// newJobsRuntime boots plugins and resolveAndBindJobQueue so OnQueueBound
// fences apply. retry/cancel/requeue must use this bound value — never a
// second raw JobQueue when a plugin RegisterQueue'd a different backend.
func newJobsRuntime(cfg *config.Config, log logger.Logger) (*jobsCLIRuntime, error) {
	if cfg.Queue.Driver != "postgres" {
		return nil, fmt.Errorf("jobs:* commands require queue.driver=postgres — job introspection/retry/cancel/requeue has no equivalent for %q (Postgres-queue-only; see internal/domain/jobs/reader.go)", cfg.Queue.Driver)
	}
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	pluginApp := &plugin.App{
		Logger:            log,
		Config:            cfg,
		Bootstrap:         boot,
		PluginMigrateMode: plugin.PluginMigrateVerify,
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	// Same pre-InitAll wiring as serve / worker / schedule:run — plugins that
	// Require(CapIntegrationStock) (e.g. warehousedemo) fail Init on a nil syncer.
	if _, err := wireIntegrationStockSyncerFromDB(conn, pluginApp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := bindProductPorts(pluginApp, boot); err != nil {
		conn.Close()
		return nil, err
	}
	preparePermissionRegistry(pluginApp)
	summary := registry.InitAll(pluginApp)
	if err := plugin.InitSummaryError(summary); err != nil {
		conn.Close()
		return nil, err
	}
	if summary.Failed > 0 {
		conn.Close()
		return nil, fmt.Errorf("plugin init failed: %d plugin(s) failed to initialize", summary.Failed)
	}
	if err := pluginApp.AssertNeedsMetExcept(plugin.CapCart, plugin.CapCatalogSeed); err != nil {
		conn.Close()
		return nil, fmt.Errorf("plugin capabilities: %w", err)
	}
	freezePermissionRegistry(pluginApp)

	bound, err := resolveAndBindJobQueue(pluginApp, conn, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}

	reader, okR := bound.(domainjobs.Reader)
	admin, okA := bound.(domainjobs.Admin)
	if !okR || !okA {
		conn.Close()
		return nil, fmt.Errorf("jobs:*: bound queue (%T) does not implement jobs.Reader/Admin — OnQueueBound wrappers must forward those ports (refusing a second JobQueue that would bypass the fence)", bound)
	}
	svc, err := jobsApp.NewService(reader, admin)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("jobs service: %w", err)
	}
	return &jobsCLIRuntime{svc: svc, conn: conn, queue: bound}, nil
}

// runJobsList handles `app jobs:list [--type=X] [--status=Y] [--limit=N] [--offset=N] [--json]`.
func runJobsList(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
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

	svc, conn, err := newJobsIntrospection(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = log

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
func runJobsShow(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	id, jsonOut, err := parseSingleIDArgs("jobs:show", args)
	if err != nil {
		return err
	}

	svc, conn, err := newJobsIntrospection(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = log

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
	payload, err := json.MarshalIndent(job.Payload, "", "  ")
	if err != nil {
		return fmt.Errorf("jobs:show: marshal payload: %w", err)
	}

	ew := &errWriter{w: w}
	ew.printf("ID:          %s\n", job.ID)
	ew.printf("Type:        %s\n", job.Type)
	ew.printf("Status:      %s\n", job.Status)
	ew.printf("Attempts:    %d/%d\n", job.Attempts, job.MaxRetries)
	ew.printf("Run at:      %s\n", formatCLITimestamp(job.RunAt))
	ew.printf("Created at:  %s\n", formatCLITimestamp(job.CreatedAt))
	ew.printf("Updated at:  %s\n", formatCLITimestamp(job.UpdatedAt))
	if job.LastError != "" {
		ew.printf("Last error:  %s\n", job.LastError)
	}
	ew.printf("Payload:\n%s\n", payload)
	return ew.err
}

// runJobsRetry handles `app jobs:retry <id>`.
func runJobsRetry(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	id, err := parseSingleIDArg("jobs:retry", args)
	if err != nil {
		return err
	}

	rt, err := newJobsRuntime(cfg, log)
	if err != nil {
		return err
	}
	defer rt.conn.Close()

	ctx := context.Background()
	retryErr := rt.svc.Retry(ctx, id)
	auditCLIAction(ctx, rt.conn, log, adminApp.AuditJobRetry, "job", id, retryErr)
	if retryErr != nil {
		return fmt.Errorf("jobs:retry: %w", retryErr)
	}
	return writeSuccessLinef(w, "Job %s queued for retry.\n", id)
}

// runJobsRequeue handles `app jobs:requeue <id> [--min-age=5m]` —
// processing → pending without resetting attempts (bound queue /
// OnQueueBound fence). Hooks run under FOR UPDATE before pending.
func runJobsRequeue(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	id, minAge, err := parseJobsRequeueArgs(args)
	if err != nil {
		return err
	}
	rt, err := newJobsRuntime(cfg, log)
	if err != nil {
		return err
	}
	defer rt.conn.Close()
	ctx := context.Background()
	detail, err := rt.svc.Get(ctx, id)
	if err != nil {
		auditCLIAction(ctx, rt.conn, log, adminApp.AuditJobRequeue, "job", id, err)
		return fmt.Errorf("jobs:requeue: %w", err)
	}
	if detail == nil {
		notFound := fmt.Errorf("jobs:requeue: no job with id %q", id)
		auditCLIAction(ctx, rt.conn, log, adminApp.AuditJobRequeue, "job", id, notFound)
		return notFound
	}
	if detail.Status != domainjobs.StatusProcessing {
		wrongStatus := fmt.Errorf("jobs:requeue: job is %s, not processing — only a stale processing claim can be requeued (use jobs:retry for failed)", detail.Status)
		auditCLIAction(ctx, rt.conn, log, adminApp.AuditJobRequeue, "job", id, wrongStatus)
		return wrongStatus
	}
	requeueErr := rt.queue.Requeue(ctx, id, detail.Attempts, domainjobs.WithRequeueMinAge(minAge))
	auditCLIAction(ctx, rt.conn, log, adminApp.AuditJobRequeue, "job", id, requeueErr)
	if requeueErr != nil {
		switch {
		case errors.Is(requeueErr, domainjobs.ErrRequeueTooFresh):
			return fmt.Errorf("jobs:requeue: claim younger than --min-age=%s (still a plausible live handler): %w", minAge, requeueErr)
		case errors.Is(requeueErr, domainjobs.ErrRequeueInProgress):
			return fmt.Errorf("jobs:requeue: another reclaim is in progress (wait for it to finish or for the default %s lease to expire): %w", domainjobs.DefaultRequeueMinAge, requeueErr)
		case errors.Is(requeueErr, domainjobs.ErrRequeueWrongAttempt):
			return fmt.Errorf("jobs:requeue: attempt mismatch (claim moved): %w", requeueErr)
		case errors.Is(requeueErr, domainjobs.ErrRequeueNotProcessing):
			return fmt.Errorf("jobs:requeue: not processing: %w", requeueErr)
		default:
			return fmt.Errorf("jobs:requeue: %w", requeueErr)
		}
	}
	return writeSuccessLinef(w, "Job %s requeued (processing → pending, attempts=%d kept; min-age=%s).\n", id, detail.Attempts, minAge)
}

func parseJobsRequeueArgs(args []string) (id string, minAge time.Duration, err error) {
	minAge = domainjobs.DefaultRequeueMinAge
	var positional []string
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--min-age="):
			raw := strings.TrimPrefix(arg, "--min-age=")
			d, perr := time.ParseDuration(raw)
			if perr != nil || d < time.Second {
				return "", 0, fmt.Errorf("usage: app jobs:requeue <id> [--min-age=5m]: invalid --min-age %q (minimum 1s)", raw)
			}
			minAge = d
		case arg == "--min-age":
			return "", 0, fmt.Errorf("usage: app jobs:requeue <id> [--min-age=5m]")
		case strings.HasPrefix(arg, "--"):
			return "", 0, fmt.Errorf("usage: app jobs:requeue <id> [--min-age=5m]: unrecognized option %q", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return "", 0, fmt.Errorf("usage: app jobs:requeue <id> [--min-age=5m]")
	}
	return positional[0], minAge, nil
}

// runJobsCancel handles `app jobs:cancel <id>`.
func runJobsCancel(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	id, err := parseSingleIDArg("jobs:cancel", args)
	if err != nil {
		return err
	}

	rt, err := newJobsRuntime(cfg, log)
	if err != nil {
		return err
	}
	defer rt.conn.Close()

	ctx := context.Background()
	cancelErr := rt.svc.Cancel(ctx, id)
	auditCLIAction(ctx, rt.conn, log, adminApp.AuditJobCancel, "job", id, cancelErr)
	if cancelErr != nil {
		return fmt.Errorf("jobs:cancel: %w", cancelErr)
	}
	return writeSuccessLinef(w, "Job %s cancelled.\n", id)
}
