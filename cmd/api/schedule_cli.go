package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	integrationApp "github.com/akarso/shopanda/internal/application/integration"
	schedulerApp "github.com/akarso/shopanda/internal/application/scheduler"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/runtime"
)

// registerSchedulerTasks registers every core scheduled task (cache
// cleanup, cart recovery, audit retention, reservation expiry) plus any
// plugin-registered cron-triggered sync jobs onto sched. This is the
// single source of truth both runScheduler (the long-running standalone
// process, main.go) and runScheduleTrigger (the one-shot CLI invocation
// below) register from — previously each had its own copy of this list,
// which meant a task added to one and forgotten in the other would make
// `schedule:trigger <name>` return a misleading "no such task" for a task
// that plainly exists in `schedule:list`/`./app scheduler`, with no
// compiler or test signal. One function, called from both, closes that
// drift risk structurally instead of relying on remembering to keep two
// lists in sync.
func registerSchedulerTasks(pluginApp *plugin.App, jobQueue domainjobs.Queue, log logger.Logger, sched domainscheduler.Scheduler) error {
	runtime.RegisterCacheCleanup(jobQueue, cacheApp.JobType, log, sched)
	runtime.RegisterCartRecovery(jobQueue, log, sched)
	runtime.RegisterAuditRetention(jobQueue, log, sched)
	runtime.RegisterReservationExpiry(jobQueue, log, sched)
	if err := integrationApp.RegisterSyncJobCronTriggers(pluginApp, jobQueue, sched, log); err != nil {
		return fmt.Errorf("sync job cron triggers: %w", err)
	}
	return nil
}

// newSchedulerService opens a standalone DB connection and constructs the
// same schedulerApp.Service the HTTP admin API (schedule_admin.go) calls
// for List/SetEnabled — both are always Postgres-backed (see
// domainscheduler.Catalog's doc comment), so this works correctly
// regardless of which process is actually running the scheduler. Not used
// for Trigger — see runScheduleTrigger's own comment for why. Returns the
// open conn too (not just a closer) so mutating commands can build an
// audit-log repository off the same connection.
func newSchedulerService(cfg *config.Config) (svc *schedulerApp.Service, conn *sql.DB, err error) {
	dsn := config.DatabaseDSN(cfg)
	conn, err = db.Open(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w", err)
	}
	store, err := postgres.NewSchedulerStore(conn)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("scheduler store: %w", err)
	}
	svc, err = schedulerApp.NewService(store)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("scheduler service: %w", err)
	}
	return svc, conn, nil
}

// runScheduleList handles `app schedule:list [--json]`.
func runScheduleList(w io.Writer, cfg *config.Config, _ logger.Logger, args []string) error {
	jsonOut := false
	for _, arg := range args {
		if arg != "--json" {
			return fmt.Errorf("schedule:list: unknown argument %q (usage: schedule:list [--json])", arg)
		}
		jsonOut = true
	}

	svc, conn, err := newSchedulerService(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	entries, err := svc.List(context.Background())
	if err != nil {
		return fmt.Errorf("schedule:list: %w", err)
	}
	return formatSchedulesList(w, entries, jsonOut)
}

func formatSchedulesList(w io.Writer, entries []domainscheduler.CatalogEntry, jsonOut bool) error {
	if jsonOut {
		out := make([]map[string]interface{}, len(entries))
		for i, e := range entries {
			out[i] = scheduleEntryToJSON(e)
		}
		return writeJSON(w, out)
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "No scheduled tasks registered.")
		return err
	}
	tw := newColumnWriter(w, "NAME", "SPEC", "NEXT RUN", "ENABLED")
	for _, e := range entries {
		tw.row(e.Name, e.Spec, formatCLITimestamp(e.NextRun), fmt.Sprintf("%t", e.Enabled))
	}
	return tw.flush()
}

// scheduleEntryToJSON mirrors internal/interfaces/http/admin/schedule_admin.go's
// toScheduleResponses field-for-field (snake_case keys) — see
// jobSummaryToJSON's comment for why this is a separate, duplicated
// mapping rather than a shared import.
func scheduleEntryToJSON(e domainscheduler.CatalogEntry) map[string]interface{} {
	return map[string]interface{}{
		"name":     e.Name,
		"spec":     e.Spec,
		"next_run": formatCLITimestampJSON(e.NextRun),
		"enabled":  e.Enabled,
	}
}

// runScheduleEnable handles `app schedule:enable <name>`.
func runScheduleEnable(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	return runScheduleSetEnabled(w, cfg, log, args, "schedule:enable", adminApp.AuditScheduleEnable, true)
}

// runScheduleDisable handles `app schedule:disable <name>`.
func runScheduleDisable(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	return runScheduleSetEnabled(w, cfg, log, args, "schedule:disable", adminApp.AuditScheduleDisable, false)
}

func runScheduleSetEnabled(w io.Writer, cfg *config.Config, log logger.Logger, args []string, command string, action adminApp.AuditAction, enabled bool) error {
	name, err := parseSingleIDArg(command, args)
	if err != nil {
		return err
	}

	svc, conn, err := newSchedulerService(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	setErr := svc.SetEnabled(ctx, name, enabled)
	auditCLIAction(ctx, conn, log, action, "schedule", name, setErr)
	if setErr != nil {
		return fmt.Errorf("%s: %w", command, setErr)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Fprintf(w, "Schedule %q %s.\n", name, state)
	return nil
}

// runScheduleTrigger handles `app schedule:trigger <name>`.
//
// This deliberately does NOT go through schedulerApp.Service.Trigger (the
// same call job_admin.go/schedule_admin.go use) — that path requires a
// live scheduler embedded in the calling process (see
// domainscheduler.Catalog.Trigger's doc comment and PR-1030's "Design
// correction"), and a standalone CLI invocation never has one, so it
// would always return a CodeConflict ("no embedded scheduler") no matter
// what. That's the architecturally correct answer for a `serve` process
// without an embedded scheduler, but it would make this command useless
// for its actual purpose (firing a task on demand from a script, without
// waiting for `dev` mode or the next real tick).
//
// Instead this command becomes its own short-lived local scheduler for
// the duration of one Trigger call: it registers every task the
// standalone `scheduler` process would via the shared
// registerSchedulerTasks (mirroring runScheduler's setup exactly,
// including plugin-registered cron jobs — see that function's own
// comment for why this is a single shared list, not two that could
// drift), then calls TriggerLocal directly — the same underlying method
// SchedulerStore.Trigger calls when a LocalTrigger *is* attached. This
// keeps "decides whether triggering is allowed and fires the same fn" as
// one code path (TriggerLocal), matching the PR's "no separate business
// logic path" principle at the level that actually matters — it does not
// mean literally every command must reuse the same currently-embedded
// scheduler instance, which no CLI invocation can ever have.
//
// Running the full plugin bootstrap (registry.InitAll, permission
// registry freeze, stock syncer wiring) here to fire one named task is a
// real, known cost: any plugin whose Init() has side effects (webhook
// registration, background goroutines, external calls) pays them on
// every CLI trigger invocation, even for plugins wholly unrelated to the
// target schedule. This mirrors runScheduler's own construction (not a
// new pattern), and is necessary for schedule:trigger to be able to
// trigger a plugin-registered cron task at all — but it's worth knowing
// before scripting frequent, automated schedule:trigger calls in CI
// (RUNBOOK.md documents this explicitly).
//
// sched.Stop() after TriggerLocal blocks (via its internal wg.Wait())
// until the fired task's fn has actually run — necessary here because,
// unlike the long-running scheduler process, this CLI process would
// otherwise exit (and race the async-fired goroutine) the moment this
// function returns.
func runScheduleTrigger(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	name, err := parseSingleIDArg("schedule:trigger", args)
	if err != nil {
		return err
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		return err
	}
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: boot,
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	if err := wireIntegrationStockSyncerFromDB(conn, pluginApp); err != nil {
		return err
	}
	preparePermissionRegistry(pluginApp)
	if summary := registry.InitAll(pluginApp); summary.Failed > 0 {
		return fmt.Errorf("plugin init failed: %d plugin(s) failed to initialize", summary.Failed)
	}
	freezePermissionRegistry(pluginApp)

	jobQueue, err := postgres.NewJobQueue(conn)
	if err != nil {
		return fmt.Errorf("job queue: %w", err)
	}
	schedulerStore, err := postgres.NewSchedulerStore(conn)
	if err != nil {
		return fmt.Errorf("scheduler store: %w", err)
	}
	sched := cron.New(log, cron.WithStore(schedulerStore))
	if err := registerSchedulerTasks(pluginApp, jobQueue, log, sched); err != nil {
		return err
	}

	triggerErr := sched.TriggerLocal(name)
	sched.Stop() // waits for the fired task's fn to actually complete before this process exits

	ctx := context.Background()
	auditCLIAction(ctx, conn, log, adminApp.AuditScheduleTrigger, "schedule", name, triggerErr)
	if triggerErr != nil {
		return fmt.Errorf("schedule:trigger: %w", triggerErr)
	}
	fmt.Fprintf(w, "Schedule %q triggered.\n", name)
	return nil
}
