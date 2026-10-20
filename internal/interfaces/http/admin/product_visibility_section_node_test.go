package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAdminHandler_ProductVisibilitySectionScript(t *testing.T) {
	h := newAdminHandler(t)

	indexRec := httptest.NewRecorder()
	h.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	index := indexRec.Body.String()
	uiPos := strings.Index(index, `src="/admin/product_visibility_section.js"`)
	adminPos := strings.Index(index, `src="/admin/admin.js"`)
	if uiPos < 0 || adminPos < 0 || uiPos > adminPos {
		t.Fatal("index.html must load product_visibility_section.js before admin.js")
	}

	jsRec := httptest.NewRecorder()
	h.ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/admin/product_visibility_section.js", nil))
	if jsRec.Code != http.StatusOK {
		t.Fatalf("product_visibility_section.js status = %d", jsRec.Code)
	}
	body := jsRec.Body.String()
	for _, expected := range []string{
		"ShopandaProductVisibilitySection",
		"modesFromProduct",
		"stateFromVisibilityPayload",
		"badgeText",
		"showFailures",
		"renderSectionHTML",
		"collectModes",
		"bindAxisChange",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in product_visibility_section.js", expected)
		}
	}
}

func TestProductVisibilitySection_Node(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node not available, skipping: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", "product_visibility_section_test.js")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test product_visibility_section_test.js: %v\n%s", err, out)
	}
}

// TestFailureLabelsMatchAutoBasisCodes keeps JS FAILURE_LABELS keys in sync
// with catalog.AutoBasisCode* constants in visibility.go.
func TestFailureLabelsMatchAutoBasisCodes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node not available, skipping: %v", err)
	}

	goCodes := parseAutoBasisCodesFromVisibilityGo(t)
	sort.Strings(goCodes)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	script := `const m = require("./dist/product_visibility_section.js"); ` +
		`console.log(JSON.stringify(Object.keys(m.FAILURE_LABELS).sort()));`
	cmd := exec.CommandContext(ctx, node, "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node -e: %v\n%s", err, out)
	}

	var jsKeys []string
	if err := json.Unmarshal(bytes.TrimSpace(out), &jsKeys); err != nil {
		t.Fatalf("unmarshal node output %q: %v", out, err)
	}

	if !reflect.DeepEqual(jsKeys, goCodes) {
		t.Fatalf("product_visibility_section.js FAILURE_LABELS keys = %v, want %v (parsed from catalog.AutoBasisCode* constants in visibility.go)", jsKeys, goCodes)
	}
}

// parseAutoBasisCodesFromVisibilityGo reads
// internal/domain/catalog/visibility.go (located relative to this test
// file's own path via runtime.Caller, so it works regardless of the test
// runner's working directory) and extracts the string value of every
// AutoBasisCode* constant with a simple regex. No import of the catalog
// package's constants themselves is used here deliberately — the point of
// this test is to catch a rename/add/remove in the *source text*, the same
// signal a human reviewer would see in a diff.
func parseAutoBasisCodesFromVisibilityGo(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's path")
	}
	// internal/interfaces/http/admin/product_visibility_section_node_test.go
	// -> up three directories -> internal/domain/catalog/visibility.go
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "domain", "catalog", "visibility.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	re := regexp.MustCompile(`AutoBasisCode\w+\s*=\s*"([^"]+)"`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatalf("no AutoBasisCode* constants found in %s — regex or path is stale", path)
	}
	codes := make([]string, 0, len(matches))
	for _, m := range matches {
		codes = append(codes, m[1])
	}
	return codes
}
