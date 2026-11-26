package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/infrastructure/email"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// defaultEmailTemplatesDir is the relative path under the process data root
// (cwd, else Docker /app) for transactional HTML templates (PR-1210).
const defaultEmailTemplatesDir = "templates/emails"

// dockerAppRoot matches Dockerfile WORKDIR when the binary lives under /usr/local/bin.
const dockerAppRoot = "/app"

// loadFileEmailTemplates overwrites builtin registrations with files from dir
// and returns the shared layout wrapper. Missing dir keeps builtins (warn).
// A present directory with a parse error fails boot (returned error).
func loadFileEmailTemplates(t *mail.Templates, dir string, log logger.Logger) (mail.Layout, error) {
	if t == nil {
		return nil, fmt.Errorf("email templates: nil registry")
	}
	if dir == "" {
		dir = defaultEmailTemplatesDir
	}

	resolved, found, err := resolveEmailTemplatesDir(dir)
	if err != nil {
		return nil, err
	}
	if !found {
		if log != nil {
			log.Warn("email.templates_dir_missing", map[string]interface{}{
				"dir":      dir,
				"resolved": resolved,
				"reason":   "using builtin RegisterTemplates only",
			})
		}
		return nil, nil
	}

	loader, layout, abs, err := email.Setup(resolved)
	if err != nil {
		return nil, err
	}
	names, err := email.LoadDir(t, loader)
	if err != nil {
		return nil, err
	}
	if log != nil {
		sort.Strings(names)
		fields := map[string]interface{}{
			"dir":   abs,
			"names": strings.Join(names, ","),
			"count": len(names),
		}
		if len(names) == 0 {
			log.Warn("email.templates_dir_empty", fields)
		} else {
			log.Info("email.templates_loaded", fields)
		}
	}
	return layout, nil
}

// resolveEmailTemplatesDir finds a templates directory. Relative paths try
// cwd first (same contract as migrations/ and themes/), then /app/<rel> so a
// Docker process whose cwd is not WORKDIR still finds the shipped tree.
func resolveEmailTemplatesDir(dir string) (resolved string, found bool, err error) {
	if filepath.IsAbs(dir) {
		st, statErr := os.Stat(dir)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return dir, false, nil
			}
			return "", false, fmt.Errorf("email templates dir: %w", statErr)
		}
		if !st.IsDir() {
			return "", false, fmt.Errorf("email templates: %s is not a directory", dir)
		}
		abs, absErr := filepath.Abs(dir)
		if absErr != nil {
			return "", false, fmt.Errorf("email templates abs: %w", absErr)
		}
		return abs, true, nil
	}

	candidates := []string{dir}
	if !strings.HasPrefix(dir, dockerAppRoot+string(os.PathSeparator)) && dir != dockerAppRoot {
		candidates = append(candidates, filepath.Join(dockerAppRoot, dir))
	}

	var lastMissing string
	for _, c := range candidates {
		st, statErr := os.Stat(c)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				lastMissing = c
				continue
			}
			return "", false, fmt.Errorf("email templates dir: %w", statErr)
		}
		if !st.IsDir() {
			return "", false, fmt.Errorf("email templates: %s is not a directory", c)
		}
		abs, absErr := filepath.Abs(c)
		if absErr != nil {
			return "", false, fmt.Errorf("email templates abs: %w", absErr)
		}
		return abs, true, nil
	}
	if lastMissing == "" {
		lastMissing = dir
	}
	abs, absErr := filepath.Abs(lastMissing)
	if absErr != nil {
		return lastMissing, false, nil
	}
	return abs, false, nil
}
