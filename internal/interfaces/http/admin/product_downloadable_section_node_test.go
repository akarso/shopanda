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

func TestAdminHandler_DownloadableSectionScript(t *testing.T) {
	h := newAdminHandler(t)
	indexRec := httptest.NewRecorder()
	h.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	index := indexRec.Body.String()
	dlPos := strings.Index(index, `src="/admin/product_downloadable_section.js"`)
	grantPos := strings.Index(index, `src="/admin/download_grant_admin_ui.js"`)
	adminPos := strings.Index(index, `src="/admin/admin.js"`)
	if dlPos < 0 || grantPos < 0 || adminPos < 0 || dlPos > adminPos || grantPos > adminPos {
		t.Fatal("index.html must load downloadable section and grant UI before admin.js")
	}
	jsRec := httptest.NewRecorder()
	h.ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/admin/product_downloadable_section.js", nil))
	if jsRec.Code != http.StatusOK {
		t.Fatalf("product_downloadable_section.js status = %d", jsRec.Code)
	}
	body := jsRec.Body.String()
	for _, expected := range []string{"ShopandaProductDownloadableSection", "renderDownloadableSectionHTML", "filesTableHTML"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in product_downloadable_section.js", expected)
		}
	}
}

func TestProductDownloadableSection_Node(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for downloadable section tests: %v", err)
	}
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", "product_downloadable_section_test.js", "download_grant_admin_ui_test.js")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test downloadable ui: %v\n%s", err, out)
	}
}
