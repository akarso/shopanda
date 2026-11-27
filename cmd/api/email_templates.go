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

// emailTemplatesFallbackRoots is tried after a relative cwd miss (tests may override).
var emailTemplatesFallbackRoots = []string{dockerAppRoot}

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

	resolved, found, tried, err := resolveEmailTemplatesDir(dir, log)
	if err != nil {
		return nil, err
	}
	if !found {
		if log != nil {
			log.Warn("email.templates_dir_missing", map[string]interface{}{
				"dir":      dir,
				"tried":    strings.Join(tried, ","),
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
	layoutLoaded := email.HasLayout(abs)
	if log != nil {
		sort.Strings(names)
		fields := map[string]interface{}{
			"dir":           abs,
			"names":         strings.Join(names, ","),
			"count":         len(names),
			"layout_loaded": layoutLoaded,
			"tried":         strings.Join(tried, ","),
		}
		if len(names) == 0 {
			log.Warn("email.templates_dir_empty", fields)
		} else {
			log.Info("email.templates_loaded", fields)
		}
	}
	return layout, nil
}

// resolveEmailTemplatesDir finds a templates directory with at least one body
// file. Relative paths try cwd first (same contract as migrations/themes/),
// then /app/<rel>. Empty dirs, non-directories, and unreadable relative
// candidates are misses so Docker can still resolve /app/templates/emails.
func resolveEmailTemplatesDir(dir string, log logger.Logger) (resolved string, found bool, tried []string, err error) {
	if filepath.IsAbs(dir) {
		abs, ok, why, absErr := probeEmailTemplatesCandidate(dir)
		if absErr != nil {
			return "", false, []string{dir}, absErr
		}
		tried = []string{annotateTried(dir, why)}
		if !ok {
			return abs, false, tried, nil
		}
		return abs, true, tried, nil
	}

	candidates := []string{dir}
	for _, root := range emailTemplatesFallbackRoots {
		if root == "" {
			continue
		}
		fb := filepath.Join(root, dir)
		if fb == dir || strings.HasPrefix(dir, root+string(os.PathSeparator)) {
			continue
		}
		candidates = append(candidates, fb)
	}

	var lastAbs string
	for _, c := range candidates {
		abs, ok, why, probeErr := probeEmailTemplatesCandidate(c)
		if probeErr != nil {
			// Relative tree: skip unreadable/non-dir and keep trying /app.
			tried = append(tried, annotateTried(c, probeErr.Error()))
			if log != nil {
				log.Warn("email.templates_candidate_skipped", map[string]interface{}{
					"candidate": c,
					"reason":    probeErr.Error(),
				})
			}
			continue
		}
		tried = append(tried, annotateTried(c, why))
		lastAbs = abs
		if ok {
			return abs, true, tried, nil
		}
	}
	if lastAbs == "" {
		lastAbs = dir
		if abs, absErr := filepath.Abs(candidates[len(candidates)-1]); absErr == nil {
			lastAbs = abs
		}
	}
	return lastAbs, false, tried, nil
}

func annotateTried(candidate, why string) string {
	if why == "" {
		return candidate
	}
	return candidate + " (" + why + ")"
}

// probeEmailTemplatesCandidate returns abs path, whether it is usable (dir with
// ≥1 body template), and a short why string for logs.
func probeEmailTemplatesCandidate(path string) (abs string, ok bool, why string, err error) {
	st, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			abs, _ = filepath.Abs(path)
			return abs, false, "missing", nil
		}
		return "", false, "", fmt.Errorf("email templates dir: %w", statErr)
	}
	if !st.IsDir() {
		return "", false, "", fmt.Errorf("email templates: %s is not a directory", path)
	}
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		return "", false, "", fmt.Errorf("email templates abs: %w", absErr)
	}
	n, countErr := email.CountBodyTemplates(abs)
	if countErr != nil {
		return "", false, "", fmt.Errorf("email templates dir: %w", countErr)
	}
	if n == 0 {
		return abs, false, "empty", nil
	}
	return abs, true, fmt.Sprintf("bodies=%d layout=%v", n, email.HasLayout(abs)), nil
}
