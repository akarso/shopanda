package theme_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	themeapp "github.com/akarso/shopanda/internal/application/theme"
	domtheme "github.com/akarso/shopanda/internal/domain/theme"
)

func TestStamp_ChangesWhenTemplateRewritten(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: watch\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "templates", "home.html")
	if err := os.WriteFile(page, []byte("{{ define \"content\" }}one{{ end }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := themeapp.Stamp(dir)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if err := os.WriteFile(page, []byte("{{ define \"content\" }}two{{ end }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := themeapp.Stamp(dir)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if second <= first {
		t.Fatalf("stamp did not advance: first=%d second=%d", first, second)
	}
}

func writeWatchThemePair(t *testing.T) (parent, child string) {
	t.Helper()
	root := t.TempDir()
	parent = filepath.Join(root, "parent")
	child = filepath.Join(root, "child")
	if err := os.MkdirAll(filepath.Join(parent, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "theme.yaml"), []byte("name: parent\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "templates", "home.html"), []byte("{{ define \"content\" }}p{{ end }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "theme.yaml"), []byte("name: child\nversion: 0.1.0\nparent: ../parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return parent, child
}

func TestStamp_ChildThemeYAMLOnly(t *testing.T) {
	_, child := writeWatchThemePair(t)
	first, err := themeapp.Stamp(child)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if first == 0 {
		t.Fatal("expected non-zero stamp for child theme.yaml")
	}
	time.Sleep(5 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(child, "theme.yaml"), []byte("name: child-renamed\nversion: 0.1.0\nparent: ../parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := themeapp.Stamp(child)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if second <= first {
		t.Fatalf("child theme.yaml edit did not advance stamp: first=%d second=%d", first, second)
	}
}

func TestWatchLoop_StopsOnCancel(t *testing.T) {
	_, child := writeWatchThemePair(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		themeapp.WatchLoop(ctx, child, 15*time.Millisecond, nil, nil, nil)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WatchLoop did not return after cancel")
	}
}

func TestWatchLoop_ReloadsOnThemeYAML(t *testing.T) {
	_, child := writeWatchThemePair(t)
	reloaded := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go themeapp.WatchLoop(ctx, child, 15*time.Millisecond, nil, func(e *domtheme.Engine) {
		if e != nil {
			reloaded <- e.Theme().Name
		}
	}, nil)
	time.Sleep(5 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(child, "theme.yaml"), []byte("name: reloaded\nversion: 0.2.0\nparent: ../parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-reloaded:
		if name != "reloaded" {
			t.Fatalf("reloaded name = %q, want reloaded", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WatchLoop did not reload after theme.yaml change")
	}
}
