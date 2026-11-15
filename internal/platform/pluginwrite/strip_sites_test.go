package pluginwrite_test

import (
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// RequiredStripSites are dispatchers that invoke foreign/plugin callbacks with a
// context.Context and must strip the plugin-write grant first. Add an entry when
// introducing another such dispatcher. This does not discover new sites automatically.
var requiredStripSites = []struct {
	relPath string
	needle  string
}{
	{"internal/platform/event/bus.go", "pluginwrite.Strip(ctx)"},
	{"internal/domain/pricing/pipeline.go", "pluginwrite.Strip(ctx)"},
	{"internal/domain/jobs/worker.go", "pluginwrite.Strip(ctx)"},
	{"internal/application/checkout/workflow.go", "StripPluginWrite(ctx)"},
	// AddItem only: other cart paths rely on pricing.Pipeline.Execute for strip.
	{"internal/application/cart/service.go", "StripPluginWrite(ctx)"},
}

func TestRequiredStripSitesPresent(t *testing.T) {
	root := repoRoot(t)
	for _, site := range requiredStripSites {
		path := filepath.Join(root, site.relPath)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", site.relPath, err)
		}
		if !hasActiveNeedle(string(raw), site.needle) {
			t.Errorf("%s: missing active (non-comment) %q — trust must be stripped at this dispatcher", site.relPath, site.needle)
		}
	}
}

func TestHasActiveNeedle_IgnoresCommentsAndStrings(t *testing.T) {
	needle := "pluginwrite.Strip(ctx)"
	if hasActiveNeedle("// "+needle+"\n", needle) {
		t.Fatal("line-commented call must not count")
	}
	if hasActiveNeedle("/*\n"+needle+"\n*/\n", needle) {
		t.Fatal("block-commented call on its own line must not count")
	}
	if hasActiveNeedle("var s = `"+needle+"`\n", needle) {
		t.Fatal("needle inside a raw string must not count")
	}
	if hasActiveNeedle(`var s = "`+needle+`"`+"\n", needle) {
		t.Fatal("needle inside a double-quoted string must not count")
	}
	if !hasActiveNeedle("\tctx = "+needle+" // strip trust\n", needle) {
		t.Fatal("trailing line comment must not hide the call")
	}
	if !hasActiveNeedle("\tctx = "+needle+"\n", needle) {
		t.Fatal("active call must count")
	}
}

// hasActiveNeedle reports whether needle appears as Go code (not in comments or string literals).
func hasActiveNeedle(src, needle string) bool {
	fset := token.NewFileSet()
	file := fset.AddFile("src.go", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil, scanner.ScanComments)

	var b strings.Builder
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT || tok == token.STRING {
			continue
		}
		if lit != "" {
			b.WriteString(lit)
			continue
		}
		b.WriteString(tok.String())
	}
	return strings.Contains(b.String(), needle)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
