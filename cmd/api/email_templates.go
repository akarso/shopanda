package main

import (
	"fmt"
	"os"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/infrastructure/email"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// defaultEmailTemplatesDir is the cwd-relative path for transactional HTML
// templates (PR-1210). Docker WORKDIR is /app and the image ships this tree.
const defaultEmailTemplatesDir = "templates/emails"

// loadFileEmailTemplates overwrites builtin registrations with files from dir
// and returns the shared layout wrapper. Missing dir keeps builtins (warn).
func loadFileEmailTemplates(t *mail.Templates, dir string, log logger.Logger) (mail.Layout, error) {
	if t == nil {
		return nil, fmt.Errorf("email templates: nil registry")
	}
	if dir == "" {
		dir = defaultEmailTemplatesDir
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if log != nil {
				log.Warn("email.templates_dir_missing", map[string]interface{}{
					"dir":    dir,
					"reason": "using builtin RegisterTemplates only",
				})
			}
			return nil, nil
		}
		return nil, fmt.Errorf("email templates dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("email templates: %s is not a directory", dir)
	}

	loader, layout, _, err := email.Setup(dir)
	if err != nil {
		return nil, err
	}
	if err := email.LoadDir(t, loader); err != nil {
		return nil, err
	}
	if log != nil {
		log.Info("email.templates_loaded", map[string]interface{}{"dir": dir})
	}
	return layout, nil
}
