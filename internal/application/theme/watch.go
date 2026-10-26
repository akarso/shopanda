package theme

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	domtheme "github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/infrastructure/themefs"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Stamp is a signature of theme.yaml, layout.yaml, and templates along the
// full parent chain: paths, sizes, and mtimes. Deleting an override or
// editing an ancestor layout.yaml changes the stamp even when the newest
// remaining mtime is unchanged. Static files are excluded — FileServer
// reads them from disk without a reload.
func Stamp(dir string) (string, error) {
	chain, err := themefs.ThemeChain(dir)
	if err != nil {
		return "", err
	}
	var parts []string
	consider := func(path string) {
		if path == "" {
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		parts = append(parts, fmt.Sprintf("%s\t%d\t%d", path, info.Size(), info.ModTime().UnixNano()))
	}
	for _, themeDir := range chain {
		consider(filepath.Join(themeDir, "theme.yaml"))
		consider(filepath.Join(themeDir, "layout.yaml"))
		matches, err := filepath.Glob(filepath.Join(themeDir, "templates", "*.html"))
		if err != nil {
			continue
		}
		for _, path := range matches {
			consider(path)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n"), nil
}

// WatchLoop reloads the theme when template metadata changes.
// It is intended for SHOPANDA_DEV_MODE only.
func WatchLoop(ctx context.Context, dir string, interval time.Duration, opts []domtheme.Option, onReload func(*domtheme.Engine), log logger.Logger) {
	if interval <= 0 {
		interval = time.Second
	}
	stamp, err := Stamp(dir)
	if err != nil {
		if log != nil {
			log.Warn("theme.watch.initial_stamp_failed", map[string]interface{}{
				"theme_path": dir,
				"error":      err.Error(),
			})
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := Stamp(dir)
			if err != nil {
				if log != nil {
					log.Warn("theme.watch.stamp_failed", map[string]interface{}{
						"theme_path": dir,
						"error":      err.Error(),
					})
				}
				continue
			}
			if next == "" || next == stamp {
				continue
			}
			engine, loadErr := Load(dir, opts...)
			if loadErr != nil {
				if log != nil {
					log.Warn("theme.watch.reload_failed", map[string]interface{}{
						"theme_path": dir,
						"error":      loadErr.Error(),
					})
				}
				continue
			}
			stamp = next
			if onReload != nil {
				onReload(engine)
			}
			if log != nil {
				log.Info("theme.watch.reloaded", map[string]interface{}{
					"theme_path": dir,
					"name":       engine.Theme().Name,
				})
			}
		}
	}
}
