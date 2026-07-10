package themefs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/themefs"
)

func TestResolveRootTheme_LayoutInheritance(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(root, "child")
	for _, dir := range []string{parent, child} {
		if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(parent, "theme.yaml"), "name: parent\nversion: 0.1.0\n")
	writeFile(t, filepath.Join(parent, "layout.yaml"), `version: "1"
containers:
  pdp.info:
    blocks: [meta, description, actions]
`)
	writeFile(t, filepath.Join(parent, "templates", "product.html"), "{{ define \"content\" }}p{{ end }}\n")
	writeFile(t, filepath.Join(parent, "templates", "layout.html"), "<html>{{ template \"content\" . }}</html>\n")

	writeFile(t, filepath.Join(child, "theme.yaml"), "name: child\nversion: 0.1.0\nparent: ../parent\n")
	writeFile(t, filepath.Join(child, "layout.yaml"), `version: "1"
containers:
  pdp.info:
    blocks: [actions, meta]
`)
	writeFile(t, filepath.Join(child, "templates", "product.html"), "{{ define \"content\" }}c{{ end }}\n")

	resolved, _, err := themefs.ResolveRootTheme(child)
	if err != nil {
		t.Fatalf("ResolveRootTheme: %v", err)
	}
	got := resolved.Layout.Containers["pdp.info"].Blocks
	want := []string{"actions", "meta"}
	if len(got) != len(want) {
		t.Fatalf("blocks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blocks = %v, want %v", got, want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
