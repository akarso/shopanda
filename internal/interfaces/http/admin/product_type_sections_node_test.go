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

func TestAdminHandler_ProductTypeSectionsScript(t *testing.T) {
	h := newAdminHandler(t)

	indexRec := httptest.NewRecorder()
	h.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	index := indexRec.Body.String()
	uiPos := strings.Index(index, `src="/admin/product_type_sections.js"`)
	adminPos := strings.Index(index, `src="/admin/admin.js"`)
	if uiPos < 0 || adminPos < 0 || uiPos > adminPos {
		t.Fatal("index.html must load product_type_sections.js before admin.js")
	}

	jsRec := httptest.NewRecorder()
	h.ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/admin/product_type_sections.js", nil))
	if jsRec.Code != http.StatusOK {
		t.Fatalf("product_type_sections.js status = %d", jsRec.Code)
	}
	body := jsRec.Body.String()
	for _, expected := range []string{
		"ShopandaProductTypeSections",
		"sectionVisible",
		"syncSections",
		"selectNeedsUnknownOption",
		"not a duplicated map",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in product_type_sections.js", expected)
		}
	}
}

func TestProductTypeSections_Node(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for product type section tests: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", "product_type_sections_test.js")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test product_type_sections_test.js: %v\n%s", err, out)
	}
}
