package themefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/akarso/shopanda/internal/domain/theme"
	"gopkg.in/yaml.v3"
)

func loadLayoutYAMLOptional(path string) (theme.LayoutConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return theme.LayoutConfig{}, nil
		}
		return theme.LayoutConfig{}, fmt.Errorf("themefs: open layout.yaml: %w", err)
	}
	defer f.Close()

	var layout theme.LayoutConfig
	if err := yaml.NewDecoder(f).Decode(&layout); err != nil {
		return theme.LayoutConfig{}, fmt.Errorf("themefs: decode layout.yaml: %w", err)
	}
	return layout, nil
}

func overlayLayoutFile(themeDir string, merged *theme.ResolvedTemplates) error {
	if merged == nil {
		return nil
	}
	layout, err := loadLayoutYAMLOptional(filepath.Join(themeDir, "layout.yaml"))
	if err != nil {
		return err
	}
	merged.Layout = theme.OverlayLayoutConfig(merged.Layout, layout)
	return nil
}
