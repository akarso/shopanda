package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	adminuserApp "github.com/akarso/shopanda/internal/application/adminuser"
	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	integrationApp "github.com/akarso/shopanda/internal/application/integration"
	inventoryApp "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/internal/application/notification"
	setupApp "github.com/akarso/shopanda/internal/application/setup"
	slotsApp "github.com/akarso/shopanda/internal/application/slots"
	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
	"github.com/akarso/shopanda/internal/platform/migrate"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/runtime"
	"github.com/akarso/shopanda/internal/platform/tracing"
	"github.com/akarso/shopanda/internal/seed"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"

	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	result, err := config.Load(config.FindConfigFile())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := result.Config

	log := logger.New(cfg.Log.Level)

	if result.DotEnvUsed {
		log.Warn("app.config.dotenv", map[string]interface{}{
			"path": result.DotEnvPath,
			"message": ".env file loaded — this is a development convenience; " +
				"in production, prefer configs/config.yaml or export variables in your shell " +
				"before starting the binary",
		})
	}

	log.Info("app.config.loaded", map[string]interface{}{
		"config": cfg.String(),
	})

	// Subcommand dispatch.
	if len(os.Args) > 1 {
		var pluginCLIRegistry *plugin.Registry
		pluginCLIRegistryFn := func() *plugin.Registry {
			if pluginCLIRegistry == nil {
				pluginCLIRegistry = bootstrapPluginCLIRegistry(cfg, log)
			}
			return pluginCLIRegistry
		}
		switch os.Args[1] {
		case "help":
			printHelp(cfg, log, pluginCLIRegistryFn)
			return nil
		case "setup":
			return runSetup(cfg, log)
		case "migrate":
			return runMigrate(cfg, log)
		case "serve":
			return runServe(cfg, log, false)
		case "dev":
			return runServe(cfg, log, cfg.Dev.EmbedScheduler)
		case "worker":
			return runWorker(cfg, log)
		case "scheduler":
			return runScheduler(cfg, log)
		case "seed":
			return runSeed(cfg, log)
		case "search:reindex":
			return runSearchReindex(cfg, log)
		case "config:export":
			return runConfigExport(cfg, log)
		case "config:import":
			return runConfigImport(cfg, log)
		case "import:products":
			return runImportProducts(cfg, log)
		case "export:products":
			return runExportProducts(cfg, log)
		case "import:stock":
			return runImportStock(cfg, log)
		case "export:stock":
			return runExportStock(cfg, log)
		case "import:customers":
			return runImportCustomers(cfg, log)
		case "export:customers":
			return runExportCustomers(cfg, log)
		case "import:attributes":
			return runImportAttributes(cfg, log)
		case "export:attributes":
			return runExportAttributes(cfg, log)
		case "import:categories":
			return runImportCategories(cfg, log)
		case "export:categories":
			return runExportCategories(cfg, log)
		case "import:prices":
			return runImportPrices(cfg, log)
		case "export:prices":
			return runExportPrices(cfg, log)
		case "export:epr":
			return runExportEpr(cfg, log)
		case "export:oss":
			return runExportOss(cfg, log)
		case "jobs:list":
			return runJobsList(os.Stdout, cfg, log, os.Args[2:])
		case "jobs:show":
			return runJobsShow(os.Stdout, cfg, log, os.Args[2:])
		case "jobs:retry":
			return runJobsRetry(os.Stdout, cfg, log, os.Args[2:])
		case "jobs:cancel":
			return runJobsCancel(os.Stdout, cfg, log, os.Args[2:])
		case "schedule:list":
			return runScheduleList(os.Stdout, cfg, log, os.Args[2:])
		case "schedule:trigger":
			return runScheduleTrigger(os.Stdout, cfg, log, os.Args[2:])
		case "schedule:enable":
			return runScheduleEnable(os.Stdout, cfg, log, os.Args[2:])
		case "schedule:disable":
			return runScheduleDisable(os.Stdout, cfg, log, os.Args[2:])
		case "plugins":
			if len(os.Args) < 3 {
				return fmt.Errorf("usage: app plugins report [--json]")
			}
			switch os.Args[2] {
			case "report":
				return runPluginsReport(os.Stdout, cfg, log, os.Args[3:])
			default:
				return fmt.Errorf("unknown plugins command: %s (try: plugins report)", os.Args[2])
			}
		default:
			if ran, err := runPluginCLICommand(cfg, log, pluginCLIRegistryFn, os.Args[1], os.Args[2:]); err != nil {
				return err
			} else if ran {
				return nil
			}
			return fmt.Errorf("unknown command: %s (run 'help' for usage)", os.Args[1])
		}
	}

	// Default: start HTTP server (production-style, no embedded scheduler).
	return runServe(cfg, log, false)
}

func runServe(cfg *config.Config, log logger.Logger, embedScheduler bool) error {
	// Database.
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	repos, err := openServeRepos(conn)
	if err != nil {
		return err
	}

	// Tracing must be set up before wireServeRuntime (which constructs the
	// checkout Workflow) and buildServeHandler (which constructs
	// TracingMiddleware): both resolve their otel.Tracer(...) handle once,
	// at construction time, and only get a real exporter if the global SDK
	// provider is already installed by then (see workflow.go's NewWorkflow
	// / shared.TracingMiddleware for why a handle obtained before Setup
	// runs would otherwise stay bound to the no-op default forever).
	tracingShutdown, err := tracing.Setup(context.Background(), cfg.Tracing, "shopanda-api")
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	// Bounded best-effort flush for every early-return path below, once
	// Setup has installed a real provider — without this, a startup
	// failure after that point would leak the batch exporter's background
	// goroutine and drop any spans already buffered (unlikely to be more
	// than a handful this early, but not zero: process exit via
	// os.Exit(1) in main() doesn't run this either way, so it's cheap
	// insurance against that assumption ever changing rather than a fix
	// for an observed problem).
	shutdownTracing := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracingShutdown(ctx); err != nil {
			log.Error("tracing.shutdown.failed", err, nil)
		}
	}

	metricsRecorder, metricsHandler := newMetrics(cfg)

	rt, err := wireServeRuntime(cfg, log, conn, repos, metricsRecorder)
	if err != nil {
		shutdownTracing()
		return err // UnbindRuntime already run inside wireServeRuntime
	}

	handler, err := buildServeHandler(cfg, log, rt, conn)
	if err != nil {
		shutdownTracing()
		rbac.UnbindRuntime()
		return err
	}

	srv := shophttp.NewServer(
		cfg.Server.Host,
		cfg.Server.Port,
		handler,
		log,
	)

	var sched scheduler.Scheduler
	var schedulerCancel context.CancelFunc
	var schedulerDone chan struct{}
	if embedScheduler {
		// PR-1030: attach the Postgres-backed scheduler store (already
		// constructed in wireServeRuntime for the admin API's List/
		// SetEnabled, which must work regardless of whether a scheduler is
		// embedded here) so this process's registrations/overrides are
		// visible admin-side, and hand the store a LocalTrigger reference
		// so the admin API's Trigger action actually works from this
		// process — it stays a no-op-returning-conflict in a `serve`
		// process that doesn't embed a scheduler (production default).
		cronSched := cron.New(log, cron.WithStore(rt.schedulerStore))
		rt.schedulerStore.SetLocalTrigger(cronSched)
		sched = cronSched
		runtime.RegisterCacheCleanup(rt.jobQueue, cacheApp.JobType, log, sched)
		runtime.RegisterCartRecovery(rt.jobQueue, log, sched)
		runtime.RegisterAuditRetention(rt.jobQueue, log, sched)
		runtime.RegisterReservationExpiry(rt.jobQueue, log, sched)
		if err := integrationApp.RegisterSyncJobCronTriggers(rt.pluginApp, rt.jobQueue, sched, log); err != nil {
			shutdownTracing()
			rbac.UnbindRuntime()
			return fmt.Errorf("sync job cron triggers: %w", err)
		}
		schedulerCtx, cancel := context.WithCancel(context.Background())
		schedulerCancel = cancel
		schedulerDone = make(chan struct{})
		go func() {
			defer close(schedulerDone)
			sched.Start(schedulerCtx)
		}()
		log.Info("app.dev.scheduler.embedded", nil)
	}

	// Start job worker in background.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		rt.jobWorker.Start(workerCtx)
		close(workerDone)
	}()

	// Metrics: dedicated listener, only started when metrics.enabled (see
	// configs/config.example.yaml). Scrapes are stateless, so an abrupt
	// Close() on shutdown is sufficient — no drain grace needed.
	metricsSrv, metricsDone, err := startMetricsServer(cfg, metricsHandler, log)
	if err != nil {
		// Scheduler/worker are already running goroutines at this point —
		// cancel and wait for them (bounded) instead of leaking them into a
		// process that returns an error but doesn't necessarily exit.
		earlyCancels := []func(){workerCancel}
		earlyDones := []<-chan struct{}{workerDone}
		if schedulerCancel != nil {
			earlyCancels = append(earlyCancels, schedulerCancel)
			earlyDones = append(earlyDones, schedulerDone)
		}
		runtime.ShutdownBackground(log, 10*time.Second, sched, earlyCancels, earlyDones)
		shutdownTracing()
		rbac.UnbindRuntime()
		return fmt.Errorf("metrics: %w", err)
	}

	// Block until server shuts down (handles SIGINT/SIGTERM internally).
	err = srv.ListenAndServe()

	var cancels []func()
	var dones []<-chan struct{}
	if schedulerCancel != nil {
		cancels = append(cancels, schedulerCancel)
		dones = append(dones, schedulerDone)
	}
	cancels = append(cancels, workerCancel)
	dones = append(dones, workerDone)
	if metricsSrv != nil {
		cancels = append(cancels, func() { metricsSrv.Close() })
		dones = append(dones, metricsDone)
	}

	const backgroundTimeout = 10 * time.Second
	// Extra wait so Drain can log event.bus.drain.timeout before process exit.
	// Drain's 10s is the handler budget; this slack is not extra handler grace.
	const drainWaitSlack = time.Second
	rt.bus.BeginShutdown()
	busDone := make(chan struct{})
	go func() {
		defer close(busDone)
		rt.bus.Drain(backgroundTimeout)
	}()
	dones = append(dones, busDone)
	runtime.ShutdownBackground(log, backgroundTimeout+drainWaitSlack, sched, cancels, dones)

	// Flush any spans still buffered by the batch processor, after
	// everything else has stopped so shutdown-path spans (if any future
	// instrumentation adds them) aren't dropped.
	shutdownTracing()

	return err
}

func runSetup(cfg *config.Config, log logger.Logger) error {
	skipSeed := false
	demoSeed := false
	verbose := false

	for _, arg := range os.Args[2:] {
		switch arg {
		case "--skip-seed":
			skipSeed = true
		case "--demo-seed":
			demoSeed = true
		case "--verbose":
			verbose = true
		case "--non-interactive":
			// Accepted for forward compatibility; currently the default.
		case "--help", "-h":
			fmt.Println(`Usage: shopanda setup [flags]

Flags:
  --skip-seed          Skip the seeding step
  --demo-seed          Populate demo compliance metadata on seed catalog products
  --verbose            Print structured log entries during setup
  --non-interactive    Use env vars only, no prompts (default)
  --help, -h           Show this help`)
			return nil
		default:
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("setup: unknown flag %q (boolean flags do not accept =value syntax)", arg)
			}
			return fmt.Errorf("setup: unexpected argument %q", arg)
		}
	}

	// Step 1: Database connectivity.
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("setup: database: %w", err)
	}
	defer conn.Close()
	fmt.Println("✓ Database connected")
	if verbose {
		log.Info("setup.db.connected", map[string]interface{}{
			"host":     cfg.Database.Host,
			"port":     cfg.Database.Port,
			"database": cfg.Database.Name,
		})
	}

	// Step 2: Migrations.
	applied, err := migrate.Run(conn, "migrations")
	if err != nil {
		return fmt.Errorf("setup: migrate: %w", err)
	}
	if applied > 0 {
		fmt.Printf("✓ %d migrations applied\n", applied)
	} else {
		fmt.Println("✓ Migrations up to date")
	}
	if verbose {
		log.Info("setup.migrate", map[string]interface{}{"applied": applied})
	}

	// Step 3: Seeders.
	if skipSeed {
		fmt.Println("– Seeding skipped (--skip-seed)")
	} else {
		reg := seed.NewRegistry()
		registerDefaultSeeders(reg)

		deps := seed.Deps{DB: conn, Logger: log, DemoData: demoSeed}
		result, seedErr := reg.Run(context.Background(), deps)
		if seedErr != nil {
			return fmt.Errorf("setup: seed: %w", seedErr)
		}
		fmt.Printf("✓ Seed complete (executed: %d, skipped: %d)\n",
			result.Executed, result.Skipped)
		if verbose {
			log.Info("setup.seed", map[string]interface{}{
				"executed": result.Executed,
				"skipped":  result.Skipped,
			})
		}
	}

	// Summary.
	baseURL := cfg.Server.PublicBaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}
	fmt.Println()
	fmt.Printf("Store is ready at %s\n", baseURL)
	fmt.Printf("Admin API: %s/api/v1/admin\n", baseURL)
	fmt.Printf("API Docs:  %s/docs\n", baseURL)

	return nil
}

func runMigrate(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	log.Info("migrate.start", nil)

	applied, err := migrate.Run(conn, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	log.Info("migrate.complete", map[string]interface{}{
		"applied": applied,
	})
	return nil
}

func runScheduler(cfg *config.Config, log logger.Logger) error {
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
	freezePermissionRegistry(pluginApp) // scheduler: freeze only (no BindRuntime; multi-command process)

	jobQueue, err := postgres.NewJobQueue(conn)
	if err != nil {
		return fmt.Errorf("job queue: %w", err)
	}
	// PR-1030: the standalone scheduler process is the one that's actually
	// running in production (RUNTIME_MODES.md), so it's the one that must
	// write its registrations and read enable/disable overrides through
	// the same Postgres table the admin API (hosted by `serve`, a
	// different process) reads/writes — no LocalTrigger attached here,
	// since there's no HTTP surface in this process for it to serve.
	schedulerStore, err := postgres.NewSchedulerStore(conn)
	if err != nil {
		return fmt.Errorf("scheduler store: %w", err)
	}
	var sched scheduler.Scheduler = cron.New(log, cron.WithStore(schedulerStore))
	if err := registerSchedulerTasks(pluginApp, jobQueue, log, sched); err != nil {
		return err
	}

	// Block until interrupted (context cancelled via signal).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for shutdown signals (same as server).
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("scheduler.shutdown.signal", nil)
		cancel()
	}()

	defer sched.Stop()
	sched.Start(ctx)
	return nil
}

func runConfigExport(cfg *config.Config, log logger.Logger) error {
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	repo := postgres.NewConfigRepo(conn)
	entries, err := repo.All(context.Background())
	if err != nil {
		return fmt.Errorf("config export: %w", err)
	}

	// Build flat map keyed by full dot-notation keys.
	// This avoids ambiguity between branch maps and map-valued leaves.
	root := make(map[string]interface{}, len(entries))
	for _, e := range entries {
		if _, dup := root[e.Key]; dup {
			return fmt.Errorf("config export: duplicate key %q", e.Key)
		}
		root[e.Key] = e.Value
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("config export: marshal: %w", err)
	}
	fmt.Print(string(out))
	return nil
}

func runConfigImport(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app config:import <file.yaml>")
	}
	filePath := os.Args[2]

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("config import: read %s: %w", filePath, err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("config import: parse %s: %w", filePath, err)
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("config import: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	repo := postgres.NewConfigRepo(tx)
	var count int
	for k, v := range raw {
		if err := repo.Set(ctx, k, v); err != nil {
			return fmt.Errorf("config import: set %q: %w", k, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("config import: commit: %w", err)
	}

	log.Info("config.import.complete", map[string]interface{}{
		"file":    filePath,
		"entries": count,
	})
	return nil
}

func runSeed(cfg *config.Config, log logger.Logger) error {
	demoData := false
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--demo-seed":
			demoData = true
		case "--help", "-h":
			fmt.Println(`Usage: shopanda seed [flags]

Flags:
  --demo-seed   Populate demo compliance metadata on seed catalog products
  --help, -h    Show this help`)
			return nil
		default:
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("seed: unknown flag %q", arg)
			}
			return fmt.Errorf("seed: unexpected argument %q", arg)
		}
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	log.Info("seed.start", nil)

	reg := seed.NewRegistry()
	registerDefaultSeeders(reg)

	deps := seed.Deps{
		DB:       conn,
		Logger:   log,
		DemoData: demoData,
	}

	result, err := reg.Run(context.Background(), deps)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	log.Info("seed.complete", map[string]interface{}{
		"executed": result.Executed,
		"skipped":  result.Skipped,
	})

	return nil
}

func registerDefaultSeeders(reg *seed.Registry) {
	reg.Register(&seed.ConfigSeeder{})
	reg.Register(&seed.StoreSeeder{})
	reg.Register(&seed.TaxSeeder{})
	reg.Register(&seed.AdminSeeder{})
	reg.Register(&seed.CatalogSeeder{})
	reg.Register(&seed.WeeeAttributesSeeder{})
	reg.Register(&seed.EprAttributesSeeder{})
	reg.Register(&seed.GpsrAttributesSeeder{})
}

func printHelp(cfg *config.Config, log logger.Logger, pluginCLIRegistryFn func() *plugin.Registry) {
	fmt.Print(appendPluginCLIHelp(pluginCLIRegistryFn, `Usage: app <command> [arguments]

Commands:
  dev                  Start HTTP server with embedded worker and scheduler (local dev)
  serve                Start the HTTP server with embedded worker (default)
  setup                Run first-time setup (migrate + seed + verify)
  worker               Start the background job worker
  scheduler            Start the cron scheduler
  migrate              Run database migrations
  seed                 Seed the database with initial data
  search:reindex       Re-index all products in the search engine
  config:export        Export configuration to stdout (YAML)
  config:import <file> Import configuration from a YAML file
  import:products <f>  Import products from a CSV file
  export:products <f>  Export products to a CSV file
  import:stock <f>     Import stock from a CSV file
  export:stock <f>     Export stock to a CSV file
  import:customers <f> Import customers from a CSV file
  export:customers <f> Export customers to a CSV file
  import:attributes <f> Import attributes from a CSV file
  export:attributes <f> Export attributes to a CSV file
  import:categories <f> Import categories from a CSV file
  export:categories <f> Export categories to a CSV file
  import:prices <f>    Import prices from a CSV file
  export:prices <f>    Export prices to a CSV file
  export:epr <f>       Export EPR packaging metadata ([--include-empty] <file.csv>)
  export:oss <f>       Export OSS/IOSS tax report ([--summary] [--from=YYYY-MM-DD] [--to=YYYY-MM-DD] <file.csv>)
  jobs:list            List background jobs ([--type=X] [--status=Y] [--limit=N] [--offset=N] [--json])
  jobs:show <id>       Show a job's full detail ([--json])
  jobs:retry <id>      Retry a failed job
  jobs:cancel <id>     Cancel a pending job
  schedule:list        List registered scheduled tasks ([--json])
  schedule:trigger <n> Trigger a scheduled task immediately
  schedule:enable <n>  Re-enable a scheduled task
  schedule:disable <n> Disable a scheduled task
  plugins report       Print registered extension points and ports ([--json])
  help                 Show this help message
`))
}

type storefrontOrderClaimEmailer struct {
	mailer       mail.Mailer
	storeBaseURL string
}

func (e storefrontOrderClaimEmailer) SendClaimEmail(contactEmail, claimToken string) error {
	if e.mailer == nil {
		return fmt.Errorf("storefront order claim emailer: mailer not configured")
	}
	baseURL, err := url.Parse(strings.TrimSpace(e.storeBaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return fmt.Errorf("storefront order claim emailer: invalid store base URL")
	}
	// Preserve any configured base path so deployments mounted under a
	// subpath still produce valid claim links.
	baseURL.Path = path.Join("/", baseURL.Path, "account/orders/claim")
	q := baseURL.Query()
	q.Set("claim_token", claimToken)
	baseURL.RawQuery = q.Encode()

	body := "Use the link below to claim your guest order and view it in your account:\n\n" + baseURL.String()
	return e.mailer.Send(context.Background(), mail.Message{
		To:      contactEmail,
		Subject: "Claim your guest order",
		Body:    body,
	})
}

// setupWorker creates a job queue, worker, mail handler, and cache cleanup
// handler. It returns the configured worker, the job queue (needed by
// notification services), and the cache instance.
func setupWorker(conn *sql.DB, cfg *config.Config, log logger.Logger, app *plugin.App, metricsRecorder metrics.Recorder) (*jobs.Worker, jobs.Queue, cache.Cache, error) {
	if metricsRecorder == nil {
		metricsRecorder = metrics.Noop()
	}
	jobQueue, err := resolveJobQueue(app, conn, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker := jobs.NewWorker(jobQueue, log, time.Second).WithMetrics(metricsRecorder)

	mailer, err := resolveMailer(app, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(notification.NewEmailSendHandler(mailer))

	appCache, err := resolveCache(app, conn, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	ed, ok := appCache.(cacheApp.ExpiredDeleter)
	if !ok {
		return nil, nil, nil, fmt.Errorf("cache driver %q does not support expired entry cleanup", cfg.Cache.Driver)
	}
	jobWorker.Register(cacheApp.NewCleanupHandler(ed, log))

	mailTemplates := mail.NewTemplates()
	notification.RegisterTemplates(mailTemplates)
	cartRepo, err := postgres.NewCartRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	customerRepo, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	configRepo := postgres.NewConfigRepo(conn)
	jobWorker.Register(cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     cartRepo,
		Customers: customerRepo,
		Variants:  variantRepo,
		Products:  productRepo,
		Templates: mailTemplates,
		Queue:     jobQueue,
		StoreURL:  cfg.Server.PublicBaseURL,
		Settings:  configRepo,
		Log:       log,
	}))

	auditLogRepo, err := postgres.NewAuditLogRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(adminApp.NewRetentionHandler(auditLogRepo, configRepo, log))

	reservationRepo, err := postgres.NewReservationRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(inventoryApp.NewReservationExpiryHandler(reservationRepo, log))

	merchantWebhookRepo, err := postgres.NewWebhookEndpointRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(webhookApp.NewDeliverHandler(merchantWebhookRepo, webhookApp.NewDefaultHTTPPoster(), log).WithMetrics(metricsRecorder))

	if err := integrationApp.RegisterSyncJobHandlers(app, jobWorker); err != nil {
		return nil, nil, nil, fmt.Errorf("sync job handlers: %w", err)
	}

	return jobWorker, jobQueue, appCache, nil
}

func runWorker(cfg *config.Config, log logger.Logger) error {
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
	freezePermissionRegistry(pluginApp) // worker: freeze only (no BindRuntime)

	// serve and worker are separate processes; if both enable metrics and
	// this one is still on the unmodified default, move it to the
	// documented worker port so they don't collide on the same host.
	if cfg.Metrics.Enabled && cfg.Metrics.Listen == config.DefaultMetricsListen {
		cfg.Metrics.Listen = config.DefaultWorkerMetricsListen
	}

	// Set up before setupWorker for the same reason as runServe: any future
	// job-handler instrumentation that resolves otel.Tracer(...) at
	// construction time needs the real SDK provider installed first.
	tracingShutdown, err := tracing.Setup(context.Background(), cfg.Tracing, "shopanda-worker")
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	shutdownTracing := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracingShutdown(ctx); err != nil {
			log.Error("tracing.shutdown.failed", err, nil)
		}
	}

	metricsRecorder, metricsHandler := newMetrics(cfg)
	jobWorker, _, _, err := setupWorker(conn, cfg, log, pluginApp, metricsRecorder)
	if err != nil {
		shutdownTracing()
		return err
	}

	metricsSrv, metricsDone, err := startMetricsServer(cfg, metricsHandler, log)
	if err != nil {
		shutdownTracing()
		return fmt.Errorf("metrics: %w", err)
	}

	log.Info("worker.start", nil)

	// Block until interrupted.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("worker.shutdown.signal", nil)
		cancel()
	}()

	jobWorker.Start(ctx)

	if metricsSrv != nil {
		// Mirrors runServe's drain: wait (bounded) for the metrics server's
		// own Serve goroutine to actually return after Close(), instead of
		// closing and immediately exiting the process out from under it.
		runtime.ShutdownBackground(log, 10*time.Second, nil, []func(){func() { metricsSrv.Close() }}, []<-chan struct{}{metricsDone})
	}

	shutdownTracing()
	return nil
}

func runSearchReindex(cfg *config.Config, log logger.Logger) error {
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
	preparePermissionRegistry(pluginApp)
	if summary := registry.InitAll(pluginApp); summary.Failed > 0 {
		return fmt.Errorf("plugin init failed: %d plugin(s) failed to initialize", summary.Failed)
	}
	freezePermissionRegistry(pluginApp) // search-reindex: freeze only (no BindRuntime)

	searchEngine, err := resolveSearchEngine(pluginApp, conn, cfg)
	if err != nil {
		return err
	}

	log.Info("search.reindex.start", map[string]interface{}{
		"engine": searchEngine.Name(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	// Use a repeatable-read transaction so offset-based pagination sees a
	// stable snapshot even if products are inserted/deleted concurrently.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return fmt.Errorf("search reindex: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	tmpProductRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	productRepo := tmpProductRepo.WithTx(tx)

	const batchSize = 100
	var offset, indexed int

	for {
		if err := ctx.Err(); err != nil {
			log.Info("search.reindex.interrupted", map[string]interface{}{
				"indexed": indexed,
			})
			return ctx.Err()
		}

		products, err := productRepo.List(ctx, offset, batchSize)
		if err != nil {
			if ctx.Err() != nil {
				log.Info("search.reindex.interrupted", map[string]interface{}{
					"indexed": indexed,
				})
				return ctx.Err()
			}
			return fmt.Errorf("search reindex: list products (offset=%d): %w", offset, err)
		}
		if len(products) == 0 {
			break
		}

		for _, p := range products {
			sp := search.Product{
				ID:          p.ID,
				Name:        p.Name,
				Slug:        p.Slug,
				Description: p.Description,
				CreatedAt:   p.CreatedAt,
				Attributes:  p.Attributes,
			}
			if err := searchEngine.IndexProduct(ctx, sp); err != nil {
				if ctx.Err() != nil {
					log.Info("search.reindex.interrupted", map[string]interface{}{
						"indexed": indexed,
					})
					return ctx.Err()
				}
				return fmt.Errorf("search reindex: index product %s: %w", p.ID, err)
			}
			indexed++
		}

		offset += len(products)
	}

	log.Info("search.reindex.complete", map[string]interface{}{
		"indexed": indexed,
	})

	return nil
}

type setupAdminUserCreator struct {
	svc *adminuserApp.Service
}

func (a setupAdminUserCreator) Create(ctx context.Context, in setupApp.AdminUserCreateInput) (*customer.Customer, error) {
	return a.svc.Create(ctx, adminuserApp.CreateInput{
		Email:     in.Email,
		Password:  in.Password,
		FirstName: in.FirstName,
		LastName:  in.LastName,
		Role:      in.Role,
	})
}

type slotRegistryThemeSource struct {
	reg *slotsApp.Registry
}

func (s slotRegistryThemeSource) Render(anchor, placement string, data interface{}) string {
	p, err := slotsApp.ParsePlacement(placement)
	if err != nil {
		return ""
	}
	return s.reg.Render(anchor, p, data)
}
