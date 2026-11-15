package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const extensionFieldRetireUsage = "usage: extension-field retire <code> [--purge-values]"

func newExtensionFieldService(cfg *config.Config) (svc *extensionapp.FieldService, values *postgres.ExtensionValueRepo, conn *sql.DB, err error) {
	dsn := config.DatabaseDSN(cfg)
	conn, err = db.Open(dsn)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("database: %w", err)
	}
	fieldRepo, err := postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("extension field repo: %w", err)
	}
	valueRepo, err := postgres.NewExtensionValueRepo(conn)
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("extension value repo: %w", err)
	}
	reg := extensionapp.NewRegistry()
	if err := reg.LoadPersisted(context.Background(), fieldRepo, nil); err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("load extension fields: %w", err)
	}
	return extensionapp.NewFieldService(reg, fieldRepo), valueRepo, conn, nil
}

// runExtensionFieldRetire handles `app extension-field retire <code> [--purge-values]`.
func runExtensionFieldRetire(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	code, purge, err := parseExtensionFieldRetireArgs(args)
	if err != nil {
		return err
	}

	svc, values, conn, err := newExtensionFieldService(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	opts := extensionapp.RetireOptions{PurgeValues: purge}
	if purge {
		opts.Values = values
	}
	result, retireErr := svc.Retire(ctx, code, opts)
	details := map[string]interface{}{"purge_values": purge}
	if retireErr == nil {
		details["values_purged"] = result.ValuesPurged
	}
	auditCLIActionDetails(ctx, conn, log, adminApp.AuditExtensionFieldRetire, "extension_field", code, details, retireErr)
	if retireErr != nil {
		return mapExtensionFieldRetireError(retireErr)
	}
	if purge {
		return writeSuccessLinef(w, "Retired plugin-only extension field %q (purged %d value row(s)).\n", result.Code, result.ValuesPurged)
	}
	return writeSuccessLinef(w, "Retired plugin-only extension field %q.\n", result.Code)
}

func parseExtensionFieldRetireArgs(args []string) (code string, purge bool, err error) {
	for _, arg := range args {
		switch {
		case arg == "--purge-values":
			purge = true
		case arg == "--help" || arg == "-h":
			return "", false, fmt.Errorf("%s", extensionFieldRetireUsage)
		case strings.HasPrefix(arg, "-"):
			return "", false, fmt.Errorf("extension-field retire: unknown flag %q (%s)", arg, extensionFieldRetireUsage)
		case code == "":
			code = strings.TrimSpace(arg)
		default:
			return "", false, fmt.Errorf("extension-field retire: unexpected argument %q (%s)", arg, extensionFieldRetireUsage)
		}
	}
	if code == "" {
		return "", false, fmt.Errorf("extension-field retire: missing field code (%s)", extensionFieldRetireUsage)
	}
	return code, purge, nil
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
