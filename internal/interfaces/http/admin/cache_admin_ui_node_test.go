package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAdminHandler_CacheAdminUIScript(t *testing.T) {
	h := newAdminHandler(t)

	indexRec := httptest.NewRecorder()
	h.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	index := indexRec.Body.String()
	uiPos := strings.Index(index, `src="/admin/cache_admin_ui.js"`)
	adminPos := strings.Index(index, `src="/admin/admin.js"`)
	if uiPos < 0 || adminPos < 0 || uiPos > adminPos {
		t.Fatal("index.html must load cache_admin_ui.js before admin.js")
	}

	jsRec := httptest.NewRecorder()
	h.ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/admin/cache_admin_ui.js", nil))
	if jsRec.Code != http.StatusOK {
		t.Fatalf("cache_admin_ui.js status = %d", jsRec.Code)
	}
	body := jsRec.Body.String()
	for _, expected := range []string{
		"ShopandaCacheAdminUI",
		"bindClearForm",
		"newStatsLoadGate",
		"CLEAR ALL",
		"Type CLEAR ALL to confirm a full flush.",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in cache_admin_ui.js", expected)
		}
	}
}

func TestCacheAdminUI_Node(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for cache confirm-UX tests (Node 22+; see docs/guides/DEVELOPER.md): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", "cache_admin_ui_test.js")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test cache_admin_ui_test.js: %v\n%s", err, out)
	}
}
