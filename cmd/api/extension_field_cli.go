package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

const (
	extensionFieldRetireUsage   = "usage: extension-field retire <code> [--purge-values --confirm-idle]"
	extensionFieldRetireTimeout = 2 * time.Minute
)

var errExtensionFieldRetireHelp = errors.New(extensionFieldRetireUsage)

type extensionFieldRetireArgs struct {
	code        string
	purge       bool
	confirmIdle bool
}

type extensionFieldRetireRuntime struct {
	svc       *extensionapp.FieldService
	store     extensionapp.PluginOnlyRetireStore
	pluginExt *extensionapp.Registry
	conn      *sql.DB
}

// openExtensionFieldRetireRuntime boots plugins under a deadline so the CLI can
// refuse codes still registered on Init. Tests may replace this function.
var openExtensionFieldRetireRuntime = defaultOpenExtensionFieldRetireRuntime

// retireBootRegisterPlugins registers the plugin set for retire boot. Tests may replace it.
var retireBootRegisterPlugins = registerPlugins

// retireBootConfig forces postgres drivers so retire does not dial Redis/RabbitMQ/Kafka/SQS.
func retireBootConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		cfg = &config.Config{}
	}
	boot := *cfg
	boot.Queue.Driver = "postgres"
	boot.Cache.Driver = "postgres"
	boot.Search.Engine = "postgres"
	return &boot
}

func defaultOpenExtensionFieldRetireRuntime(cfg *config.Config, log logger.Logger) (*extensionFieldRetireRuntime, error) {
	bootCfg := retireBootConfig(cfg)
	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	fieldRepo, err := postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("extension field repo: %w", err)
	}

	extReg := extensionapp.NewRegistry()
	plugins := plugin.NewRegistry(log)
	retireBootRegisterPlugins(plugins, bootCfg)
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	pluginApp := &plugin.App{
		Logger:            log,
		Config:            bootCfg,
		Bootstrap:         boot,
		PluginMigrateMode: plugin.PluginMigrateVerify,
	}
	pluginApp.SetExtensionRegistry(extReg)
	preparePermissionRegistry(pluginApp)

	ctx, cancel := context.WithTimeout(context.Background(), extensionFieldRetireTimeout)
	defer cancel()
	summary := plugins.InitAllContext(ctx, pluginApp)
	if err := plugin.InitSummaryError(summary); err != nil {
		conn.Close()
		return nil, err
	}
	if summary.Failed > 0 {
		conn.Close()
		return nil, fmt.Errorf("plugin init failed: %d plugin(s) failed to initialize (refuse retire while boot is incomplete)", summary.Failed)
	}
	// A finished boot that merely ran long is still usable for the registration check;
	// InitAllContext already marks any plugin that did not finish as Failed.
	freezePermissionRegistry(pluginApp)

	return &extensionFieldRetireRuntime{
		svc:       extensionapp.NewFieldService(extReg, fieldRepo),
		store:     fieldRepo,
		pluginExt: extReg,
		conn:      conn,
	}, nil
}

// runExtensionFieldRetire handles `app extension-field retire <code> [--purge-values --confirm-idle]`.
func runExtensionFieldRetire(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	parsed, err := parseExtensionFieldRetireArgs(args)
	if errors.Is(err, errExtensionFieldRetireHelp) {
		_, _ = fmt.Fprintln(w, extensionFieldRetireUsage)
		return nil
	}
	if err != nil {
		return err
	}
	// Refuse before DB/plugin boot so a flag mistake does not dial brokers.
	if parsed.purge && !parsed.confirmIdle {
		return fmt.Errorf("extension-field retire: --purge-values requires --confirm-idle (stop serve and worker first; see RUNBOOK)")
	}

	rt, err := openExtensionFieldRetireRuntime(cfg, log)
	if err != nil {
		return err
	}
	defer rt.conn.Close()

	audit := func(retireErr error, details map[string]interface{}, valuesPurged int64) {
		if details == nil {
			details = map[string]interface{}{}
		}
		details["purge_values"] = parsed.purge
		details["confirm_idle"] = parsed.confirmIdle
		details["values_purged"] = valuesPurged
		auditCLIActionDetails(context.Background(), rt.conn, log, adminApp.AuditExtensionFieldRetire, "extension_field", parsed.code, details, retireErr)
	}

	if _, ok := rt.pluginExt.Get(parsed.code); ok {
		owner := rt.pluginExt.Owner(parsed.code)
		if owner == "" {
			owner = "(unowned)"
		}
		err := fmt.Errorf("extension-field retire: plugin still registers %q on boot (owner %s); remove or disable that plugin in this binary/config before retiring", parsed.code, owner)
		audit(err, map[string]interface{}{"plugin_registered": true, "owner": owner}, 0)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), extensionFieldRetireTimeout)
	defer cancel()

	result, retireErr := rt.svc.Retire(ctx, parsed.code, extensionapp.RetireOptions{
		PurgeValues: parsed.purge,
		Store:       rt.store,
	})
	audit(retireErr, map[string]interface{}{"purge_only": result.PurgeOnly}, result.ValuesPurged)
	if retireErr != nil {
		return mapExtensionFieldRetireError(retireErr)
	}
	if result.PurgeOnly {
		return writeSuccessLinef(w, "Purged %d value row(s) for already-retired plugin-only extension field %q.\n", result.ValuesPurged, result.Code)
	}
	if parsed.purge {
		return writeSuccessLinef(w, "Retired plugin-only extension field %q (purged %d value row(s)).\n", result.Code, result.ValuesPurged)
	}
	return writeSuccessLinef(w, "Retired plugin-only extension field %q.\n", result.Code)
}

func parseExtensionFieldRetireArgs(args []string) (extensionFieldRetireArgs, error) {
	var out extensionFieldRetireArgs
	for _, arg := range args {
		switch {
		case arg == "--purge-values":
			out.purge = true
		case arg == "--confirm-idle":
			out.confirmIdle = true
		case arg == "--help" || arg == "-h":
			return extensionFieldRetireArgs{}, errExtensionFieldRetireHelp
		case strings.HasPrefix(arg, "-"):
			return extensionFieldRetireArgs{}, fmt.Errorf("extension-field retire: unknown flag %q (%s)", arg, extensionFieldRetireUsage)
		case out.code == "":
			out.code = strings.TrimSpace(arg)
		default:
			return extensionFieldRetireArgs{}, fmt.Errorf("extension-field retire: unexpected argument %q (%s)", arg, extensionFieldRetireUsage)
		}
	}
	if out.code == "" {
		return extensionFieldRetireArgs{}, fmt.Errorf("extension-field retire: missing field code (%s)", extensionFieldRetireUsage)
	}
	return out, nil
}

func mapExtensionFieldRetireError(err error) error {
	switch {
	case errors.Is(err, extensionapp.ErrFieldNotFound):
		return fmt.Errorf("extension-field retire: field not found")
	case errors.Is(err, extensionapp.ErrFieldNotPluginOnly):
		return fmt.Errorf("extension-field retire: field is not plugin-only (use admin DELETE for ordinary fields)")
	case errors.Is(err, domainext.ErrPurgeIncomplete):
		return fmt.Errorf("extension-field retire: %w (re-run with --purge-values --confirm-idle)", err)
	default:
		return fmt.Errorf("extension-field retire: %w", err)
	}
}
