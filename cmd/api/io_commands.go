package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func runImportProducts(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("import:products")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioImport,
		Path:          path,
		StartEvent:    "import.start",
		CompleteEvent: "import.complete",
		RowErrorEvent: "import.row_error",
		WithHooks:     true,
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		productRepo, err := postgres.NewProductRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("product repo: %w", err)
		}
		variantRepo, err := postgres.NewVariantRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("variant repo: %w", err)
		}
		result, err := importer.NewProductImporter(productRepo, variantRepo, conn).
			WithRowHooks(regs.Import).
			Import(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("import: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"products": result.Products,
				"variants": result.Variants,
				"skipped":  result.Skipped,
				"errors":   len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runExportProducts(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("export:products")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          path,
		StartEvent:    "export.start",
		CompleteEvent: "export.complete",
		RowErrorEvent: "export.row_error",
		WithHooks:     true,
		AtomicExport:  false, // preserve historical direct Create behavior
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		productRepo, err := postgres.NewProductRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("product repo: %w", err)
		}
		variantRepo, err := postgres.NewVariantRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("variant repo: %w", err)
		}
		result, err := exporter.NewProductExporter(productRepo, variantRepo).
			WithRowHooks(regs.Export).
			Export(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("export: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"products": result.Products,
				"variants": result.Variants,
				"skipped":  result.Skipped,
				"errors":   len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runImportStock(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("import:stock")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioImport,
		Path:          path,
		StartEvent:    "import.stock.start",
		CompleteEvent: "import.stock.complete",
		RowErrorEvent: "import.stock.row_error",
		WithHooks:     true,
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		variantRepo, err := postgres.NewVariantRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("variant repo: %w", err)
		}
		stockRepo, err := postgres.NewStockRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("stock repo: %w", err)
		}
		result, err := importer.NewStockImporter(variantRepo, stockRepo).
			WithRowHooks(regs.Import).
			Import(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("import stock: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"updated": result.Updated,
				"skipped": result.Skipped,
				"errors":  len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runExportStock(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("export:stock")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          path,
		StartEvent:    "export.stock.start",
		CompleteEvent: "export.stock.complete",
		RowErrorEvent: "export.stock.row_error",
		WithHooks:     true,
		AtomicExport:  true,
		TempPrefix:    "stock-export-*.csv",
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		stockRepo, err := postgres.NewStockRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("stock repo: %w", err)
		}
		variantRepo, err := postgres.NewVariantRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("variant repo: %w", err)
		}
		result, err := exporter.NewStockExporter(stockRepo, variantRepo).
			WithRowHooks(regs.Export).
			Export(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("export stock: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"entries": result.Entries,
				"skipped": result.Skipped,
				"errors":  len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runImportCustomers(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("import:customers")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioImport,
		Path:          path,
		StartEvent:    "import.customers.start",
		CompleteEvent: "import.customers.complete",
		RowErrorEvent: "import.customers.row_error",
		WithHooks:     true,
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		customerRepo, err := postgres.NewCustomerRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("customer repo: %w", err)
		}
		result, err := importer.NewCustomerImporter(customerRepo).
			WithRowHooks(regs.Import).
			Import(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("import customers: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"created": result.Created,
				"skipped": result.Skipped,
				"errors":  len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runExportCustomers(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("export:customers")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          path,
		StartEvent:    "export.customers.start",
		CompleteEvent: "export.customers.complete",
		RowErrorEvent: "export.customers.row_error",
		WithHooks:     true,
		AtomicExport:  true,
		TempPrefix:    "customer-export-*.csv",
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		customerRepo, err := postgres.NewCustomerRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("customer repo: %w", err)
		}
		result, err := exporter.NewCustomerExporter(customerRepo).
			WithRowHooks(regs.Export).
			Export(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("export customers: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"entries": result.Entries,
				"skipped": result.Skipped,
				"errors":  len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runImportAttributes(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("import:attributes")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:                ioImport,
		Path:                path,
		StartEvent:          "import.attributes.start",
		CompleteEvent:       "import.attributes.complete",
		RowErrorEvent:       "import.attributes.row_error",
		RowErrorWarn:        true,
		FailMessage:         "import completed with %d errors",
		WithHooks:           true,
		CompleteAfterErrors: true,
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("import attributes: begin tx: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

		configRepo := postgres.NewConfigRepo(tx)
		result, err := importer.NewAttributeImporter(configRepo).
			WithRowHooks(regs.Import).
			Import(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("import attributes: %w", err)
		}
		out := &ioOutcome{
			Fields: map[string]interface{}{
				"attributes": result.Attributes,
				"groups":     result.Groups,
				"skipped":    result.Skipped,
			},
			Errors: result.Errors,
		}
		if len(result.Errors) > 0 {
			return out, nil
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("import attributes: commit: %w", err)
		}
		if err := syncDiscoveryFacetsFromDB(cfg, log, conn); err != nil {
			return nil, fmt.Errorf("import committed but discovery facet sync failed: %w", err)
		}
		return out, nil
	})
}

func runExportAttributes(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("export:attributes")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          path,
		StartEvent:    "export.attributes.start",
		CompleteEvent: "export.attributes.complete",
		RowErrorEvent: "export.attributes.row_error",
		WithHooks:     true,
		AtomicExport:  true,
		TempPrefix:    "attribute-export-*.csv",
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		configRepo := postgres.NewConfigRepo(conn)
		result, err := exporter.NewAttributeExporter(configRepo).
			WithRowHooks(regs.Export).
			Export(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("export attributes: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"entries": result.Entries,
				"skipped": result.Skipped,
				"errors":  len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runImportCategories(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("import:categories")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:                ioImport,
		Path:                path,
		StartEvent:          "import.categories.start",
		CompleteEvent:       "import.categories.complete",
		RowErrorEvent:       "import.categories.row_error",
		RowErrorWarn:        true,
		FailMessage:         "import completed with %d errors",
		WithHooks:           true,
		CompleteAfterErrors: true,
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		categoryRepo, err := postgres.NewCategoryRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("category repo: %w", err)
		}
		result, err := importer.NewCategoryImporter(categoryRepo).
			WithRowHooks(regs.Import).
			Import(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("import categories: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"created": result.Created,
				"updated": result.Updated,
				"skipped": result.Skipped,
			},
			Errors:       result.Errors,
			Warnings:     result.Warnings,
			WarningEvent: "import.categories.row_warning",
		}, nil
	})
}

func runExportCategories(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("export:categories")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          path,
		StartEvent:    "export.categories.start",
		CompleteEvent: "export.categories.complete",
		RowErrorEvent: "export.categories.row_error",
		WithHooks:     true,
		AtomicExport:  true,
		TempPrefix:    "category-export-*.csv",
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		categoryRepo, err := postgres.NewCategoryRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("category repo: %w", err)
		}
		result, err := exporter.NewCategoryExporter(categoryRepo).
			WithRowHooks(regs.Export).
			Export(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("export categories: %w", err)
		}
		out := &ioOutcome{
			Fields: map[string]interface{}{
				"entries": result.Entries,
				"skipped": result.Skipped,
				"errors":  len(result.Errors),
			},
			Errors: result.Errors,
		}
		if result.Orphans > 0 {
			orphans := result.Orphans
			out.AfterOK = func() error {
				log.Warn("export.categories.orphans", map[string]interface{}{"count": orphans})
				return nil
			}
		}
		return out, nil
	})
}

func runImportPrices(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("import:prices")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:                ioImport,
		Path:                path,
		StartEvent:          "import.prices.start",
		CompleteEvent:       "import.prices.complete",
		RowErrorEvent:       "import.prices.row_error",
		RowErrorWarn:        true,
		FailMessage:         "import completed with %d errors",
		WithHooks:           true,
		CompleteAfterErrors: true,
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		variantRepo, err := postgres.NewVariantRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("variant repo: %w", err)
		}
		priceRepo, err := postgres.NewPriceRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("price repo: %w", err)
		}
		priceHistoryRepo, err := postgres.NewPriceHistoryRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("price history repo: %w", err)
		}
		result, err := importer.NewPriceImporter(variantRepo, priceRepo, priceHistoryRepo, conn, nil).
			WithRowHooks(regs.Import).
			Import(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("import prices: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"created": result.Created,
				"updated": result.Updated,
				"skipped": result.Skipped,
			},
			Errors: result.Errors,
		}, nil
	})
}

func runExportPrices(cfg *config.Config, log logger.Logger) error {
	path, err := requireIOPath("export:prices")
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          path,
		StartEvent:    "export.prices.start",
		CompleteEvent: "export.prices.complete",
		RowErrorEvent: "export.prices.row_error",
		WithHooks:     true,
		AtomicExport:  true,
		TempPrefix:    "price-export-*.csv",
	}, func(ctx context.Context, conn *sql.DB, f *os.File, regs ioRegs) (*ioOutcome, error) {
		variantRepo, err := postgres.NewVariantRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("variant repo: %w", err)
		}
		priceRepo, err := postgres.NewPriceRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("price repo: %w", err)
		}
		result, err := exporter.NewPriceExporter(priceRepo, variantRepo).
			WithRowHooks(regs.Export).
			Export(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("export prices: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"entries": result.Entries,
				"skipped": result.Skipped,
				"errors":  len(result.Errors),
			},
			Errors: result.Errors,
		}, nil
	})
}

func runExportEpr(cfg *config.Config, log logger.Logger) error {
	filePath, includeEmpty, err := parseEprExportArgs(ioArgs())
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          filePath,
		StartEvent:    "export.epr.start",
		CompleteEvent: "export.epr.complete",
		RowErrorEvent: "export.epr.row_error",
		AtomicExport:  true,
		TempPrefix:    "epr-export-*.csv",
	}, func(ctx context.Context, conn *sql.DB, f *os.File, _ ioRegs) (*ioOutcome, error) {
		productRepo, err := postgres.NewProductRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("product repo: %w", err)
		}
		variantRepo, err := postgres.NewVariantRepo(conn)
		if err != nil {
			return nil, fmt.Errorf("variant repo: %w", err)
		}
		configRepo := postgres.NewConfigRepo(conn)
		result, err := exporter.NewEprExporter(productRepo, variantRepo, configRepo).
			Export(ctx, f, exporter.EprExportOptions{IncludeEmpty: includeEmpty})
		if err != nil {
			return nil, fmt.Errorf("export epr: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{"rows": result.Rows},
		}, nil
	})
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
	filePath, summary, from, to, err := parseOssExportArgs(ioArgs())
	if err != nil {
		return err
	}
	return runIOCommand(cfg, log, ioCommandOpts{
		Kind:          ioExport,
		Path:          filePath,
		StartEvent:    "export.oss.start",
		CompleteEvent: "export.oss.complete",
		RowErrorEvent: "export.oss.row_error",
		AtomicExport:  true,
		TempPrefix:    "oss-export-*.csv",
		StartFields:   map[string]interface{}{"summary": summary},
	}, func(ctx context.Context, conn *sql.DB, f *os.File, _ ioRegs) (*ioOutcome, error) {
		orderRepo, err := postgres.NewOrderRepo(conn)
		if err != nil {
			return nil, err
		}
		configRepo := postgres.NewConfigRepo(conn)
		result, err := exporter.NewOssExporter(orderRepo, configRepo).Export(ctx, f, exporter.OssExportOptions{
			From:    from,
			To:      to,
			Summary: summary,
		})
		if err != nil {
			return nil, fmt.Errorf("export oss: %w", err)
		}
		return &ioOutcome{
			Fields: map[string]interface{}{
				"rows":    result.Rows,
				"summary": summary,
			},
		}, nil
	})
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
