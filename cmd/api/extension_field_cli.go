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

func newExtensionFieldRetireDeps(cfg *config.Config, log logger.Logger) (svc *extensionapp.FieldService, store *postgres.ExtensionFieldRepo, pluginReg *extensionapp.Registry, conn *sql.DB, err error) {
	dsn := config.DatabaseDSN(cfg)
	conn, err = db.Open(dsn)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("database: %w", err)
	}
	fieldRepo, err := postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		conn.Close()
		return nil, nil, nil, nil, fmt.Errorf("extension field repo: %w", err)
	}

	extReg := extensionapp.NewRegistry()
	plugins := plugin.NewRegistry(log)
	registerPlugins(plugins, cfg)
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		conn.Close()
		return nil, nil, nil, nil, err
	}
	pluginApp := &plugin.App{
		Logger:            log,
		Config:            cfg,
		Bootstrap:         boot,
		PluginMigrateMode: plugin.PluginMigrateVerify,
	}
	pluginApp.SetExtensionRegistry(extReg)
	preparePermissionRegistry(pluginApp)
	summary := plugins.InitAll(pluginApp)
	if err := plugin.InitSummaryError(summary); err != nil {
		conn.Close()
		return nil, nil, nil, nil, err
	}
	// Unrelated Init failures (e.g. misconfigured storage) must not block retire:
	// we only need whichever plugins did Init to populate the extension registry.
	if summary.Failed > 0 {
		log.Warn("extension-field.retire.plugin_init_partial", map[string]interface{}{
			"failed":      summary.Failed,
			"initialized": summary.Initialized,
		})
	}
	freezePermissionRegistry(pluginApp)

	return extensionapp.NewFieldService(extReg, fieldRepo), fieldRepo, extReg, conn, nil
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
	if parsed.purge && !parsed.confirmIdle {
		return fmt.Errorf("extension-field retire: --purge-values requires --confirm-idle (stop serve and worker first; see RUNBOOK)")
	}

	svc, store, pluginExt, conn, err := newExtensionFieldRetireDeps(cfg, log)
	if err != nil {
		return err
	}
	defer conn.Close()

	if field, ok := pluginExt.Get(parsed.code); ok && field.Access.PluginOnlyWrite {
		owner := pluginExt.Owner(parsed.code)
		if owner == "" {
			owner = "(unowned)"
		}
		return fmt.Errorf("extension-field retire: plugin still registers %q on boot (owner %s); remove or disable that plugin in this binary/config before retiring", parsed.code, owner)
	}

	ctx, cancel := context.WithTimeout(context.Background(), extensionFieldRetireTimeout)
	defer cancel()

	result, retireErr := svc.Retire(ctx, parsed.code, extensionapp.RetireOptions{
		PurgeValues: parsed.purge,
		Store:       store,
	})
	details := map[string]interface{}{
		"purge_values":  parsed.purge,
		"confirm_idle":  parsed.confirmIdle,
		"values_purged": result.ValuesPurged,
	}
	auditCLIActionDetails(ctx, conn, log, adminApp.AuditExtensionFieldRetire, "extension_field", parsed.code, details, retireErr)
	if retireErr != nil {
		return mapExtensionFieldRetireError(retireErr)
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
	default:
		return fmt.Errorf("extension-field retire: %w", err)
	}
}
