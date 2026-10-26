package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"gopkg.in/yaml.v3"
)

func TestThemeInitThemesRoot(t *testing.T) {
	if got := themeInitThemesRoot(""); got != "themes" {
		t.Fatalf("empty = %q", got)
	}
	if got := themeInitThemesRoot("themes/default"); got != "themes" {
		t.Fatalf("relative = %q", got)
	}
	if got := themeInitThemesRoot("/var/shop/themes/default"); got != "/var/shop/themes" {
		t.Fatalf("abs = %q", got)
	}
}

func TestRunThemeInit_WritesYAMLAndRejectsBadNames(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Frontend.ThemePath = filepath.Join(root, "default")
	log := logger.NewWithWriter(io.Discard, "error")

	for _, name := range []string{"Bad Name", "has:colon", "../escape", "HasUpper"} {
		if err := runThemeInit(cfg, log, []string{name}); err == nil {
			t.Fatalf("expected error for name %q", name)
		}
	}

	if err := runThemeInit(cfg, log, []string{"my-store"}); err != nil {
		t.Fatalf("runThemeInit: %v", err)
	}
	dest := filepath.Join(root, "my-store")
	raw, err := os.ReadFile(filepath.Join(dest, "theme.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var meta themeInitManifest
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("yaml: %v\n%s", err, raw)
	}
	if meta.Name != "my-store" || meta.Parent != "../default" || meta.Version != "0.1.0" {
		t.Fatalf("meta = %#v", meta)
	}
	if strings.Contains(string(raw), "name: "+meta.Name+"\nname:") {
		t.Fatal("duplicate name interpolation")
	}
	if _, err := os.Stat(filepath.Join(dest, "static", "css", "custom.css")); err != nil {
		t.Fatal(err)
	}
	if err := runThemeInit(cfg, log, []string{"my-store"}); err == nil {
		t.Fatal("expected already-exists error")
	}
}
