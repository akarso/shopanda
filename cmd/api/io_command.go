package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	exportctxApp "github.com/akarso/shopanda/internal/application/exportctx"
	importctxApp "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ioKind selects import vs export file handling in runIOCommand.
type ioKind string

const (
	ioImport ioKind = "import"
	ioExport ioKind = "export"
)

// ioOpenDB opens the CLI database. Tests may override to avoid a live Postgres.
var ioOpenDB = func(dsn string) (*sql.DB, error) {
	return db.Open(dsn)
}

// ioRegs holds optional plugin row-hook registries bootstrapped by runIOCommand.
type ioRegs struct {
	Import *importctxApp.Registry
	Export *exportctxApp.Registry
}

// ioOutcome is the per-command result returned by an IO hook.
type ioOutcome struct {
	Fields       map[string]interface{}
	Errors       []string
	Warnings     []string
	WarningEvent string
	// AfterOK runs after row-level errors are checked (and after the complete
	// log when CompleteAfterErrors is false). Used today for post-success side
	// effects such as export:categories orphan warnings. Transactional work
	// that must roll back on row errors (e.g. attribute import commit + facet
	// sync) stays inside the hook so defer Rollback still applies.
	AfterOK func() error
}

// ioCommandOpts configures shared import/export CLI orchestration.
type ioCommandOpts struct {
	Kind          ioKind
	Path          string
	StartEvent    string
	CompleteEvent string
	RowErrorEvent string
	// RowErrorWarn logs row errors with Warn instead of Error.
	RowErrorWarn bool
	// FailMessage formats the fatal error when Errors is non-empty; must contain %d.
	FailMessage string
	// WithHooks bootstraps import or export plugin row-hook registries once.
	WithHooks bool
	// AtomicExport writes via CreateTemp + Rename (export only). When false, uses os.Create.
	AtomicExport bool
	TempPrefix   string
	StartFields  map[string]interface{}
	// CompleteAfterErrors logs the complete event only after row-error checks
	// and AfterOK (attributes/categories/prices import). Default false preserves
	// the common pattern: complete log, then row errors, then fail.
	CompleteAfterErrors bool
}

// ioHook runs entity-specific import/export work. Returned errors must already
// include a command-specific prefix (e.g. "export stock: %w"); the runner
// returns them as-is after optional atomic-export cleanup.
type ioHook func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error)

func requireIOPath(cmd string) (string, error) {
	if len(os.Args) < 3 {
		return "", fmt.Errorf("usage: app %s <file.csv>", cmd)
	}
	return os.Args[2], nil
}

// runIOCommand opens the CSV file and DB, runs hook, logs row errors, and
// applies atomic rename for exports when configured.
//
// Import opens the input file before connecting to Postgres so a missing path
// fails fast with an open error instead of a slow/confusing database ping.
// Export keeps the historical DB-then-file order.
func runIOCommand(cfg *config.Config, log logger.Logger, opts ioCommandOpts, hook ioHook) error {
	if opts.Path == "" {
		return fmt.Errorf("missing file path")
	}
	if opts.FailMessage == "" {
		opts.FailMessage = string(opts.Kind) + " completed with %d row-level errors"
	}

	var (
		f       *os.File
		tmpPath string
		err     error
	)

	// Import: validate/open CSV before DB (matches pre-PR-1014 runImport* order).
	if opts.Kind == ioImport {
		f, err = os.Open(opts.Path)
		if err != nil {
			return fmt.Errorf("open %s: %w", opts.Path, err)
		}
		defer f.Close()
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := ioOpenDB(dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	switch opts.Kind {
	case ioImport:
		// file already open
	case ioExport:
		if opts.AtomicExport {
			prefix := opts.TempPrefix
			if prefix == "" {
				prefix = "export-*.csv"
			}
			f, err = os.CreateTemp(filepath.Dir(opts.Path), prefix)
			if err != nil {
				return fmt.Errorf("create temp file: %w", err)
			}
			tmpPath = f.Name()
		} else {
			f, err = os.Create(opts.Path)
			if err != nil {
				return fmt.Errorf("create csv: %w", err)
			}
			defer f.Close()
		}
	default:
		return fmt.Errorf("unknown io kind %q", opts.Kind)
	}

	var regs ioRegs
	if opts.WithHooks {
		switch opts.Kind {
		case ioImport:
			regs.Import = bootstrapImportRegistry(cfg, log)
		case ioExport:
			regs.Export = bootstrapExportRegistry(cfg, log)
		}
	}

	startFields := map[string]interface{}{"file": opts.Path}
	for k, v := range opts.StartFields {
		startFields[k] = v
	}
	log.Info(opts.StartEvent, startFields)

	outcome, err := hook(context.Background(), conn, f, regs)
	if opts.Kind == ioExport && opts.AtomicExport {
		closeErr := f.Close()
		if err != nil {
			os.Remove(tmpPath)
			// Hook errors are expected to carry their own command prefix.
			return err
		}
		if closeErr != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("close temp file: %w", closeErr)
		}
		if renameErr := os.Rename(tmpPath, opts.Path); renameErr != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("rename temp file: %w", renameErr)
		}
	} else if err != nil {
		return err
	}
	if outcome == nil {
		outcome = &ioOutcome{}
	}

	fields := outcome.Fields
	if fields == nil {
		fields = map[string]interface{}{}
	}

	if !opts.CompleteAfterErrors {
		log.Info(opts.CompleteEvent, fields)
	}

	warnEvent := outcome.WarningEvent
	if warnEvent == "" {
		warnEvent = opts.RowErrorEvent
	}
	for _, w := range outcome.Warnings {
		log.Warn(warnEvent, map[string]interface{}{"warning": w})
	}

	for _, e := range outcome.Errors {
		if opts.RowErrorWarn {
			log.Warn(opts.RowErrorEvent, map[string]interface{}{"error": e})
		} else {
			log.Error(opts.RowErrorEvent, errors.New(e), map[string]interface{}{})
		}
	}
	if len(outcome.Errors) > 0 {
		return fmt.Errorf(opts.FailMessage, len(outcome.Errors))
	}

	if outcome.AfterOK != nil {
		if err := outcome.AfterOK(); err != nil {
			return err
		}
	}

	if opts.CompleteAfterErrors {
		log.Info(opts.CompleteEvent, fields)
	}
	return nil
}
