package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	cacheApp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// newCacheAdminService opens a DB connection, bootstraps plugins (so a
// redis cache driver actually registers), and constructs the same
// cacheApp.AdminService the HTTP admin API calls. L1 stores are omitted:
// this CLI process does not hold the API's in-process catalog/nav caches.
func newCacheAdminService(cfg *config.Config, log logger.Logger) (svc *cacheApp.AdminService, conn *sql.DB, err error) {
	dsn := config.DatabaseDSN(cfg)
	conn, err = db.Open(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w", err)
	}

	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: boot,
	}
	preparePermissionRegistry(pluginApp)
	if summary := registry.InitAll(pluginApp); summary.Failed > 0 {
		conn.Close()
		return nil, nil, fmt.Errorf("plugin init failed: %d plugin(s) failed to initialize", summary.Failed)
	}
	freezePermissionRegistry(pluginApp)

	c, err := resolveCache(pluginApp, conn, cfg)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("cache: %w", err)
	}
	return cacheApp.NewAdminService(c, nil), conn, nil
}

// runCacheStats handles `app cache:stats [--json]`.
func runCacheStats(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	jsonOut := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			return fmt.Errorf("cache:stats: unknown argument %q (usage: cache:stats [--json])", arg)
		}
	}

	svc, conn, err := newCacheAdminService(cfg, log)
	if err != nil {
		return err
	}
	defer conn.Close()

	snap, err := svc.Stats(context.Background())
	if err != nil {
		return fmt.Errorf("cache:stats: %w", err)
	}
	return formatCacheStats(w, snap, jsonOut)
}

func formatCacheStats(w io.Writer, snap cacheApp.Snapshot, jsonOut bool) error {
	if jsonOut {
		return writeJSON(w, snap)
	}
	ew := &errWriter{w: w}
	ew.printf("L2 backend:  %s\n", snap.L2.Backend)
	printL2Count := func(label string, n int64) {
		if snap.L2.Approximate {
			ew.printf("%s%d (approximate)\n", label, n)
			return
		}
		ew.printf("%s%d\n", label, n)
	}
	printL2Count("L2 keys:     ", snap.L2.Keys)
	if snap.L2.TagRows != nil {
		printL2Count("L2 tag rows: ", *snap.L2.TagRows)
	}
	if snap.L2.MemoryUsedBytes != nil {
		ew.printf("L2 memory:   %d bytes\n", *snap.L2.MemoryUsedBytes)
	}
	if len(snap.L1) == 0 {
		ew.printf("L1:          (none in this process — cache:stats is a standalone CLI; L1 lives in serve)\n")
	} else {
		for _, s := range snap.L1 {
			ew.printf("L1 %s: entries=%d hits=%d misses=%d\n", s.Name, s.Entries, s.Hits, s.Misses)
		}
	}
	return ew.err
}

const cacheClearUsage = "usage: cache:clear --prefix=<p> | --tag=<t> | --key=<k> | --all"

// runCacheClear handles `app cache:clear --prefix|--tag|--key|--all`.
func runCacheClear(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	req, err := parseCacheClearArgs(args)
	if err != nil {
		return err
	}

	svc, conn, err := newCacheAdminService(cfg, log)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	result, clearErr := svc.Clear(ctx, req)
	details := map[string]interface{}{"mode": string(result.Mode)}
	if result.Target != "" {
		details["target"] = result.Target
	}
	if result.Deleted != nil {
		details["deleted"] = *result.Deleted
	}
	resourceID := result.Target
	if req.All {
		resourceID = "all"
	}
	auditCLIActionDetails(ctx, conn, log, adminApp.AuditCacheClear, "cache", resourceID, details, clearErr)
	if clearErr != nil {
		return fmt.Errorf("cache:clear: %w", clearErr)
	}
	return formatCacheClear(w, result)
}

func parseCacheClearArgs(args []string) (cacheApp.ClearRequest, error) {
	var req cacheApp.ClearRequest
	for _, arg := range args {
		switch {
		case arg == "--all":
			req.All = true
		case strings.HasPrefix(arg, "--prefix="):
			req.Prefix = strings.TrimPrefix(arg, "--prefix=")
		case strings.HasPrefix(arg, "--tag="):
			req.Tag = strings.TrimPrefix(arg, "--tag=")
		case strings.HasPrefix(arg, "--key="):
			req.Key = strings.TrimPrefix(arg, "--key=")
		default:
			return cacheApp.ClearRequest{}, fmt.Errorf("cache:clear: unknown argument %q (%s)", arg, cacheClearUsage)
		}
	}
	n := 0
	if req.All {
		n++
	}
	if strings.TrimSpace(req.Prefix) != "" {
		n++
	}
	if strings.TrimSpace(req.Tag) != "" {
		n++
	}
	if strings.TrimSpace(req.Key) != "" {
		n++
	}
	if n != 1 {
		return cacheApp.ClearRequest{}, fmt.Errorf("cache:clear: %s", cacheClearUsage)
	}
	return req, nil
}

func formatCacheClear(w io.Writer, result cacheApp.ClearResult) error {
	switch result.Mode {
	case cacheApp.ClearAll:
		n := int64(0)
		if result.Deleted != nil {
			n = *result.Deleted
		}
		return writeSuccessLinef(w, "Cleared all cache entries (%d deleted).\n", n)
	case cacheApp.ClearPrefix:
		return writeSuccessLinef(w, "Cleared cache prefix %s.\n", result.Target)
	case cacheApp.ClearTag:
		n := int64(0)
		if result.Deleted != nil {
			n = *result.Deleted
		}
		return writeSuccessLinef(w, "Cleared cache tag %s (%d deleted).\n", result.Target, n)
	case cacheApp.ClearKey:
		return writeSuccessLinef(w, "Cleared cache key %s.\n", result.Target)
	default:
		return writeSuccessLinef(w, "Cache clear completed.\n")
	}
}
