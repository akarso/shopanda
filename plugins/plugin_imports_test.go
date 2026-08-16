package plugins_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/akarso/shopanda"

// Allowed shared prefixes (PR-1017 fixed allowlist).
// Sibling packages are allowed only under the same top-level plugin directory
// (e.g. plugins/b2b → plugins/b2b/groups), not plugins/core or other plugins.
var allowedSharedPrefixes = []string{
	modulePath + "/pkg/",
	modulePath + "/internal/domain/",
	modulePath + "/internal/application/",
	modulePath + "/internal/platform/",
}

var forbiddenPrefixes = []string{
	modulePath + "/internal/infrastructure/",
	modulePath + "/internal/interfaces/",
	modulePath + "/plugins/core/",
}

// TestImportBoundary enforces the PR-1017 plugin import allowlist for
// non-core plugins. plugins/core/* may import infrastructure (driver adapters).
// *_test.go files are excluded so integration tests may construct fixtures.
func TestImportBoundary(t *testing.T) {
	root := "."
	var violations []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "core" && filepath.Dir(path) == root {
				return filepath.SkipDir
			}
			if base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		pluginRoot, ok := topLevelPlugin(path)
		if !ok {
			return nil
		}
		imps, err := fileImports(path)
		if err != nil {
			return err
		}
		for _, imp := range imps {
			if !strings.HasPrefix(imp, modulePath+"/") {
				continue // stdlib / external modules
			}
			if isForbidden(imp) {
				violations = append(violations, fmt.Sprintf("%s imports forbidden %s", path, imp))
				continue
			}
			if !isAllowed(imp, pluginRoot) {
				violations = append(violations, fmt.Sprintf("%s imports disallowed %s (not on allowlist)", path, imp))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk plugins: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("plugin import boundary violations:\n  %s", strings.Join(violations, "\n  "))
	}
}

func TestImportBoundary_ForbidsCorePluginImport(t *testing.T) {
	if !isForbidden(modulePath + "/plugins/core/postgres") {
		t.Fatal("expected plugins/core/postgres to be forbidden for non-core plugins")
	}
	if isAllowed(modulePath+"/plugins/core/postgres", "b2b") {
		t.Fatal("expected plugins/core/postgres not allowed as a b2b sibling")
	}
	if !isAllowed(modulePath+"/plugins/b2b/groups", "b2b") {
		t.Fatal("expected plugins/b2b/groups allowed for b2b")
	}
	if isAllowed(modulePath+"/plugins/example", "b2b") {
		t.Fatal("expected other top-level plugins disallowed as siblings")
	}
}

func topLevelPlugin(path string) (string, bool) {
	rel := filepath.ToSlash(path)
	parts := strings.Split(rel, "/")
	if len(parts) == 0 {
		return "", false
	}
	// Walk is rooted at plugins/; first component is the plugin name.
	root := parts[0]
	if root == "" || root == "." || strings.HasSuffix(root, ".go") {
		return "", false
	}
	return root, true
}

func isForbidden(imp string) bool {
	for _, p := range forbiddenPrefixes {
		if strings.HasPrefix(imp, p) {
			return true
		}
	}
	return false
}

func isAllowed(imp string, pluginRoot string) bool {
	for _, p := range allowedSharedPrefixes {
		if strings.HasPrefix(imp, p) {
			return true
		}
	}
	siblingExact := modulePath + "/plugins/" + pluginRoot
	siblingPrefix := siblingExact + "/"
	return imp == siblingExact || strings.HasPrefix(imp, siblingPrefix)
}

func fileImports(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		out = append(out, path)
	}
	return out, nil
}
