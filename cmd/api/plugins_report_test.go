package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRunPluginsReport_TextOutput(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			CartDemo: config.CartDemoPluginConfig{Enabled: true},
		},
	}
	log := logger.New("error")
	var buf bytes.Buffer
	if err := runPluginsReport(&buf, cfg, log, nil); err != nil {
		t.Fatalf("runPluginsReport: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Plugin registration report",
		"cartdemo/reference",
		"Plugin pricing steps:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunPluginsReport_JSONFlag(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	var buf bytes.Buffer
	if err := runPluginsReport(&buf, cfg, log, []string{"--json"}); err != nil {
		t.Fatalf("runPluginsReport --json: %v", err)
	}
	if !strings.Contains(buf.String(), `"plugins"`) {
		t.Fatalf("json output = %s", buf.String())
	}
}

func TestRunPluginsReport_UnknownFlag(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	err := runPluginsReport(ioDiscardWriter{}, cfg, log, []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err = %v", err)
	}
}

type ioDiscardWriter struct{}

func (ioDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }
