package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"gopkg.in/yaml.v3"
)

var themeInitNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type themeInitManifest struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Parent  string `yaml:"parent"`
}

func themeInitThemesRoot(themePath string) string {
	themePath = strings.TrimSpace(themePath)
	if themePath == "" {
		return "themes"
	}
	parent := filepath.Dir(filepath.Clean(themePath))
	if parent == "." || parent == string(filepath.Separator) {
		return "themes"
	}
	return parent
}

func runThemeInit(cfg *config.Config, log logger.Logger, args []string) error {
	if len(args) < 1 || strings.TrimSpace(args[0]) == "" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(`Usage: shopanda theme:init <name>

Create a child theme under the themes directory next to frontend.theme_path
(or ./themes/<name> when theme_path is unset). The child inherits ../default.

Name must match [a-z0-9][a-z0-9_-]*.

Then set frontend.theme_path (or SHOPANDA_FRONTEND_THEME_PATH) to that directory
and enable the storefront.`)
		return nil
	}
	name := strings.TrimSpace(args[0])
	if !themeInitNameRE.MatchString(name) {
		return fmt.Errorf("theme:init: name must match [a-z0-9][a-z0-9_-]*")
	}
	themePath := ""
	if cfg != nil {
		themePath = cfg.Frontend.ThemePath
	}
	dest := filepath.Join(themeInitThemesRoot(themePath), name)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("theme:init: %s already exists", dest)
	}
	if err := os.MkdirAll(filepath.Join(dest, "static", "css"), 0o755); err != nil {
		return fmt.Errorf("theme:init: %w", err)
	}
	raw, err := yaml.Marshal(themeInitManifest{
		Name:    name,
		Version: "0.1.0",
		Parent:  "../default",
	})
	if err != nil {
		return fmt.Errorf("theme:init: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "theme.yaml"), raw, 0o644); err != nil {
		return fmt.Errorf("theme:init: %w", err)
	}
	custom := "/* Child-theme overrides. Parent style.css is inherited via /static/. */\n"
	if starter, err := os.ReadFile(filepath.Join(themeInitThemesRoot(themePath), "_starter", "static", "css", "custom.css")); err == nil {
		custom = string(starter)
	}
	if err := os.WriteFile(filepath.Join(dest, "static", "css", "custom.css"), []byte(custom), 0o644); err != nil {
		return fmt.Errorf("theme:init: %w", err)
	}
	if log != nil {
		log.Info("theme.init.created", map[string]interface{}{
			"path": dest,
			"name": name,
		})
	}
	fmt.Printf("Created %s (parent: ../default)\nSet frontend.theme_path: %s\n", dest, dest)
	return nil
}
