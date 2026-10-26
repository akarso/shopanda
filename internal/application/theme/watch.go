package theme

import (
	"context"
	"os"
	"path/filepath"
	"time"

	domtheme "github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/infrastructure/themefs"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Stamp is the newest mtime of theme.yaml, layout.yaml, and templates
// along the inheritance chain. Static files are excluded — FileServer
// reads them from disk without a reload.
func Stamp(dir string) (int64, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	resolved, _, err := themefs.ResolveRootTheme(dir)
	if err != nil {
		return 0, err
	}
	var newest int64
	consider := func(path string) {
		if path == "" {
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if t := info.ModTime().UnixNano(); t > newest {
			newest = t
		}
	}
	seen := map[string]struct{}{}
	markDir := func(themeDir string) {
		if themeDir == "" {
			return
		}
		if _, ok := seen[themeDir]; ok {
			return
		}
		seen[themeDir] = struct{}{}
		consider(filepath.Join(themeDir, "theme.yaml"))
		consider(filepath.Join(themeDir, "layout.yaml"))
	}
	markDir(absDir)
	if resolved.LayoutFile != "" {
		markDir(filepath.Dir(filepath.Dir(resolved.LayoutFile)))
	}
	for _, path := range resolved.PageFiles {
		markDir(filepath.Dir(filepath.Dir(path)))
		consider(path)
	}
	for _, path := range resolved.PartialFiles {
		markDir(filepath.Dir(filepath.Dir(path)))
		consider(path)
	}
	consider(resolved.LayoutFile)
	return newest, nil
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
			if next == 0 || next == stamp {
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
