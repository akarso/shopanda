package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	adminuserApp "github.com/akarso/shopanda/internal/application/adminuser"
	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/exporter"
	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/importer"
	integrationApp "github.com/akarso/shopanda/internal/application/integration"
	"github.com/akarso/shopanda/internal/application/notification"
	setupApp "github.com/akarso/shopanda/internal/application/setup"
	slotsApp "github.com/akarso/shopanda/internal/application/slots"
	webhookApp "github.com/akarso/shopanda/internal/application/webhook"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/runtime"
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

	rt, err := wireServeRuntime(cfg, log, conn, repos)
	if err != nil {
		return err
	}

	handler, err := buildServeHandler(cfg, log, rt, conn)
	if err != nil {
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
		sched = cron.New(log)
		runtime.RegisterCacheCleanup(rt.jobQueue, cacheApp.JobType, log, sched)
		runtime.RegisterCartRecovery(rt.jobQueue, log, sched)
		runtime.RegisterAuditRetention(rt.jobQueue, log, sched)
		if err := integrationApp.RegisterSyncJobCronTriggers(rt.pluginApp, rt.jobQueue, sched, log); err != nil {
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
	runtime.ShutdownBackground(log, 10*time.Second, sched, cancels, dones)

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

func runImportProducts(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:products <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	imp := importer.NewProductImporter(productRepo, variantRepo, conn).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}

	log.Info("import.complete", map[string]interface{}{
		"products": result.Products,
		"variants": result.Variants,
		"skipped":  result.Skipped,
		"errors":   len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("import.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportProducts(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:products <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	exp := exporter.NewProductExporter(productRepo, variantRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()

	log.Info("export.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), f)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	log.Info("export.complete", map[string]interface{}{
		"products": result.Products,
		"variants": result.Variants,
		"skipped":  result.Skipped,
		"errors":   len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runImportStock(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:stock <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	stockRepo, err := postgres.NewStockRepo(conn)
	if err != nil {
		return fmt.Errorf("stock repo: %w", err)
	}
	imp := importer.NewStockImporter(variantRepo, stockRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.stock.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import stock: %w", err)
	}

	log.Info("import.stock.complete", map[string]interface{}{
		"updated": result.Updated,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("import.stock.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportStock(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:stock <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	stockRepo, err := postgres.NewStockRepo(conn)
	if err != nil {
		return fmt.Errorf("stock repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	exp := exporter.NewStockExporter(stockRepo, variantRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "stock-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.stock.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export stock: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.stock.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.stock.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runImportCustomers(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:customers <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	customerRepo, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return fmt.Errorf("customer repo: %w", err)
	}
	imp := importer.NewCustomerImporter(customerRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.customers.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import customers: %w", err)
	}

	log.Info("import.customers.complete", map[string]interface{}{
		"created": result.Created,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("import.customers.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportCustomers(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:customers <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	customerRepo, err := postgres.NewCustomerRepo(conn)
	if err != nil {
		return fmt.Errorf("customer repo: %w", err)
	}
	exp := exporter.NewCustomerExporter(customerRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "customer-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.customers.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export customers: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export customers: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.customers.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.customers.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

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
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	if err := wireIntegrationStockSyncerFromDB(conn, pluginApp); err != nil {
		return err
	}
	if summary := registry.InitAll(pluginApp); summary.Failed > 0 {
		return fmt.Errorf("plugin init failed: %d plugin(s) failed to initialize", summary.Failed)
	}

	jobQueue, err := postgres.NewJobQueue(conn)
	if err != nil {
		return fmt.Errorf("job queue: %w", err)
	}
	var sched scheduler.Scheduler = cron.New(log)
	runtime.RegisterCacheCleanup(jobQueue, cacheApp.JobType, log, sched)
	runtime.RegisterCartRecovery(jobQueue, log, sched)
	runtime.RegisterAuditRetention(jobQueue, log, sched)
	if err := integrationApp.RegisterSyncJobCronTriggers(pluginApp, jobQueue, sched, log); err != nil {
		return fmt.Errorf("sync job cron triggers: %w", err)
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

func runImportAttributes(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:attributes <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("import attributes: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	configRepo := postgres.NewConfigRepo(tx)
	imp := importer.NewAttributeImporter(configRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.attributes.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(ctx, f)
	if err != nil {
		return fmt.Errorf("import attributes: %w", err)
	}

	for _, e := range result.Errors {
		log.Warn("import.attributes.row_error", map[string]interface{}{"error": e})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d errors", len(result.Errors))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("import attributes: commit: %w", err)
	}

	if err := syncDiscoveryFacetsFromDB(cfg, log, conn); err != nil {
		return fmt.Errorf("import committed but discovery facet sync failed: %w", err)
	}

	log.Info("import.attributes.complete", map[string]interface{}{
		"attributes": result.Attributes,
		"groups":     result.Groups,
		"skipped":    result.Skipped,
	})
	return nil
}

func runExportAttributes(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:attributes <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	configRepo := postgres.NewConfigRepo(conn)
	exp := exporter.NewAttributeExporter(configRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "attribute-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.attributes.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export attributes: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export attributes: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.attributes.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.attributes.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runImportCategories(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:categories <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	categoryRepo, err := postgres.NewCategoryRepo(conn)
	if err != nil {
		return fmt.Errorf("category repo: %w", err)
	}
	imp := importer.NewCategoryImporter(categoryRepo).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.categories.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import categories: %w", err)
	}

	for _, w := range result.Warnings {
		log.Warn("import.categories.row_warning", map[string]interface{}{"warning": w})
	}

	for _, e := range result.Errors {
		log.Warn("import.categories.row_error", map[string]interface{}{"error": e})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d errors", len(result.Errors))
	}

	log.Info("import.categories.complete", map[string]interface{}{
		"created": result.Created,
		"updated": result.Updated,
		"skipped": result.Skipped,
	})

	return nil
}

func runExportCategories(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:categories <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	categoryRepo, err := postgres.NewCategoryRepo(conn)
	if err != nil {
		return fmt.Errorf("category repo: %w", err)
	}
	exp := exporter.NewCategoryExporter(categoryRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "category-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.categories.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export categories: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export categories: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.categories.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.categories.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	if result.Orphans > 0 {
		log.Warn("export.categories.orphans", map[string]interface{}{
			"count": result.Orphans,
		})
	}

	return nil
}

func runImportPrices(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app import:prices <file.csv>")
	}
	filePath := os.Args[2]

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	priceRepo, err := postgres.NewPriceRepo(conn)
	if err != nil {
		return fmt.Errorf("price repo: %w", err)
	}
	priceHistoryRepo, err := postgres.NewPriceHistoryRepo(conn)
	if err != nil {
		return fmt.Errorf("price history repo: %w", err)
	}
	imp := importer.NewPriceImporter(variantRepo, priceRepo, priceHistoryRepo, conn, nil).WithRowHooks(bootstrapImportRegistry(cfg, log))

	log.Info("import.prices.start", map[string]interface{}{"file": filePath})

	result, err := imp.Import(context.Background(), f)
	if err != nil {
		return fmt.Errorf("import prices: %w", err)
	}

	for _, e := range result.Errors {
		log.Warn("import.prices.row_error", map[string]interface{}{"error": e})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("import completed with %d errors", len(result.Errors))
	}

	log.Info("import.prices.complete", map[string]interface{}{
		"created": result.Created,
		"updated": result.Updated,
		"skipped": result.Skipped,
	})

	return nil
}

func runExportPrices(cfg *config.Config, log logger.Logger) error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: app export:prices <file.csv>")
	}
	filePath := os.Args[2]

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	priceRepo, err := postgres.NewPriceRepo(conn)
	if err != nil {
		return fmt.Errorf("price repo: %w", err)
	}
	exp := exporter.NewPriceExporter(priceRepo, variantRepo).WithRowHooks(bootstrapExportRegistry(cfg, log))

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "price-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.prices.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile)
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export prices: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export prices: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.prices.complete", map[string]interface{}{
		"entries": result.Entries,
		"skipped": result.Skipped,
		"errors":  len(result.Errors),
	})

	for _, e := range result.Errors {
		log.Error("export.prices.row_error", errors.New(e), map[string]interface{}{})
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("export completed with %d row-level errors", len(result.Errors))
	}

	return nil
}

func runExportEpr(cfg *config.Config, log logger.Logger) error {
	filePath, includeEmpty, err := parseEprExportArgs(os.Args[2:])
	if err != nil {
		return err
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	productRepo, err := postgres.NewProductRepo(conn)
	if err != nil {
		return fmt.Errorf("product repo: %w", err)
	}
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("variant repo: %w", err)
	}
	configRepo := postgres.NewConfigRepo(conn)
	exp := exporter.NewEprExporter(productRepo, variantRepo, configRepo)

	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "epr-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.epr.start", map[string]interface{}{"file": filePath})

	result, err := exp.Export(context.Background(), tmpFile, exporter.EprExportOptions{IncludeEmpty: includeEmpty})
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export epr: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export epr: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.epr.complete", map[string]interface{}{
		"rows": result.Rows,
	})

	return nil
}

func parseEprExportArgs(args []string) (filePath string, includeEmpty bool, err error) {
	for _, arg := range args {
		if arg == "--include-empty" {
			includeEmpty = true
			continue
		}
		if strings.HasPrefix(arg, "--") {
			return "", false, fmt.Errorf("export:epr: unknown flag %q", arg)
		}
		if filePath != "" {
			return "", false, fmt.Errorf("export:epr: unexpected argument %q", arg)
		}
		filePath = arg
	}
	if filePath == "" {
		return "", false, fmt.Errorf("usage: app export:epr [--include-empty] <file.csv>")
	}
	return filePath, includeEmpty, nil
}

func runExportOss(cfg *config.Config, log logger.Logger) error {
	filePath, summary, from, to, err := parseOssExportArgs(os.Args[2:])
	if err != nil {
		return err
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	orderRepo, err := postgres.NewOrderRepo(conn)
	if err != nil {
		return err
	}
	configRepo := postgres.NewConfigRepo(conn)

	exp := exporter.NewOssExporter(orderRepo, configRepo)
	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), "oss-export-*.csv")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Info("export.oss.start", map[string]interface{}{"file": filePath, "summary": summary})

	result, err := exp.Export(context.Background(), tmpFile, exporter.OssExportOptions{
		From:    from,
		To:      to,
		Summary: summary,
	})
	if closeErr := tmpFile.Close(); closeErr != nil {
		os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("export oss: %w", err)
		}
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("export oss: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	log.Info("export.oss.complete", map[string]interface{}{
		"rows":    result.Rows,
		"summary": summary,
	})

	return nil
}

func parseOssExportArgs(args []string) (filePath string, summary bool, from, to time.Time, err error) {
	var fromRaw, toRaw string
	for _, arg := range args {
		switch {
		case arg == "--summary":
			summary = true
		case strings.HasPrefix(arg, "--from="):
			fromRaw = strings.TrimPrefix(arg, "--from=")
		case strings.HasPrefix(arg, "--to="):
			toRaw = strings.TrimPrefix(arg, "--to=")
		case strings.HasPrefix(arg, "--"):
			return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: unknown flag %q", arg)
		default:
			if filePath != "" {
				return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: unexpected argument %q", arg)
			}
			filePath = arg
		}
	}
	if filePath == "" {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("usage: app export:oss [--summary] [--from=YYYY-MM-DD] [--to=YYYY-MM-DD] <file.csv>")
	}

	fromDate, err := exporter.ParseReportDate(fromRaw)
	if err != nil {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: %w", err)
	}
	toDate, err := exporter.ParseReportDate(toRaw)
	if err != nil {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: %w", err)
	}
	if fromDate.IsZero() && toDate.IsZero() {
		now := time.Now().UTC()
		year, month, _ := now.Date()
		quarterStartMonth := time.Month(((int(month)-1)/3)*3 + 1)
		fromDate = time.Date(year, quarterStartMonth, 1, 0, 0, 0, 0, time.UTC)
		toDate = now
	} else if fromDate.IsZero() || toDate.IsZero() {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: --from and --to are required unless both are omitted")
	}
	toExclusive := exporter.ReportDateRangeEnd(toDate)
	if !toExclusive.After(fromDate) {
		return "", false, time.Time{}, time.Time{}, fmt.Errorf("export:oss: --to must be on or after --from")
	}
	return filePath, summary, fromDate, toExclusive, nil
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
func setupWorker(conn *sql.DB, cfg *config.Config, log logger.Logger, app *plugin.App) (*jobs.Worker, jobs.Queue, cache.Cache, error) {
	jobQueue, err := resolveJobQueue(app, conn, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker := jobs.NewWorker(jobQueue, log, time.Second)

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

	merchantWebhookRepo, err := postgres.NewWebhookEndpointRepo(conn)
	if err != nil {
		return nil, nil, nil, err
	}
	jobWorker.Register(webhookApp.NewDeliverHandler(merchantWebhookRepo, webhookApp.NewDefaultHTTPPoster(), log))

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
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	if err := wireIntegrationStockSyncerFromDB(conn, pluginApp); err != nil {
		return err
	}
	registry.InitAll(pluginApp)

	jobWorker, _, _, err := setupWorker(conn, cfg, log, pluginApp)
	if err != nil {
		return err
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
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	registry.InitAll(pluginApp)

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
