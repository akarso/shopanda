package theme_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(page, []byte("{{ define \"content\" }}changed{{ end }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := themeapp.Stamp(dir)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if second == first {
		t.Fatalf("stamp did not change after rewrite")
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
	if first == "" {
		t.Fatal("expected stamp for child theme.yaml")
	}
	time.Sleep(5 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(child, "theme.yaml"), []byte("name: child-renamed\nversion: 0.1.0\nparent: ../parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := themeapp.Stamp(child)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if second == first {
		t.Fatal("child theme.yaml edit did not change stamp")
	}
}

func TestStamp_ChangesWhenChildOverrideDeleted(t *testing.T) {
	parent, child := writeWatchThemePair(t)
	override := filepath.Join(child, "templates", "home.html")
	if err := os.MkdirAll(filepath.Join(child, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte("{{ define \"content\" }}child{{ end }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the parent template newer so a max-mtime stamp would miss the delete.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(parent, "templates", "home.html"), later, later); err != nil {
		t.Fatal(err)
	}
	first, err := themeapp.Stamp(child)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if err := os.Remove(override); err != nil {
		t.Fatal(err)
	}
	second, err := themeapp.Stamp(child)
	if err != nil {
		t.Fatalf("Stamp after delete: %v", err)
	}
	if second == first {
		t.Fatal("deleting a child override must change the stamp")
	}
}

func TestStamp_IncludesAncestorLayoutYAML(t *testing.T) {
	root := t.TempDir()
	grand := filepath.Join(root, "grand")
	mid := filepath.Join(root, "mid")
	child := filepath.Join(root, "child")
	for _, d := range []string{filepath.Join(grand, "templates"), mid, child} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(grand, "theme.yaml"), []byte("name: grand\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grand, "templates", "home.html"), []byte("{{ define \"content\" }}g{{ end }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mid, "theme.yaml"), []byte("name: mid\nversion: 0.1.0\nparent: ../grand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mid, "layout.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "theme.yaml"), []byte("name: child\nversion: 0.1.0\nparent: ../mid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := themeapp.Stamp(child)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mid, "layout.yaml"), []byte("version: \"1\"\nslots: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := themeapp.Stamp(child)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if second == first {
		t.Fatal("ancestor layout.yaml edit must change the stamp")
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
	path := filepath.Join(child, "theme.yaml")
	deadline := time.Now().Add(2 * time.Second)
	for n := 1; ; n++ {
		select {
		case name := <-reloaded:
			if name != "reloaded" {
				t.Fatalf("reloaded name = %q, want reloaded", name)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("WatchLoop did not reload after theme.yaml change")
		}
		content := fmt.Sprintf("name: reloaded\nversion: 0.2.0\nparent: ../parent\n# %s\n", strings.Repeat("x", n))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
