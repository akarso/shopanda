package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAdminHandler_LinkedSectionScript(t *testing.T) {
	h := newAdminHandler(t)
	indexRec := httptest.NewRecorder()
	h.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	index := indexRec.Body.String()
	uiPos := strings.Index(index, `src="/admin/product_linked_section.js"`)
	assignPos := strings.Index(index, `src="/admin/assignment_admin_ui.js"`)
	adminPos := strings.Index(index, `src="/admin/admin.js"`)
	if uiPos < 0 || assignPos < 0 || adminPos < 0 || uiPos > adminPos || assignPos > adminPos {
		t.Fatal("index.html must load product_linked_section.js and assignment_admin_ui.js before admin.js")
	}
	jsRec := httptest.NewRecorder()
	h.ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/admin/product_linked_section.js", nil))
	if jsRec.Code != http.StatusOK {
		t.Fatalf("product_linked_section.js status = %d", jsRec.Code)
	}
	body := jsRec.Body.String()
	for _, expected := range []string{"ShopandaProductLinkedSection", "renderLinkedSectionHTML", "parentsTableHTML"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in product_linked_section.js", expected)
		}
	}
}

func TestProductLinkedSection_Node(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for linked section tests: %v", err)
	}
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", "product_linked_section_test.js", "assignment_admin_ui_test.js")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test linked/assignment ui: %v\n%s", err, out)
	}
}
