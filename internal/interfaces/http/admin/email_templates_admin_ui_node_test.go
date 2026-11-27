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

func TestAdminHandler_EmailTemplatesAdminUIScript(t *testing.T) {
	h := newAdminHandler(t)

	indexRec := httptest.NewRecorder()
	h.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	index := indexRec.Body.String()
	uiPos := strings.Index(index, `src="/admin/email_templates_admin_ui.js"`)
	adminPos := strings.Index(index, `src="/admin/admin.js"`)
	navPos := strings.Index(index, `href="/admin/content/email-templates"`)
	if uiPos < 0 || adminPos < 0 || uiPos > adminPos {
		t.Fatal("index.html must load email_templates_admin_ui.js before admin.js")
	}
	if navPos < 0 {
		t.Fatal("index.html must include Email Templates nav link")
	}

	jsRec := httptest.NewRecorder()
	h.ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/admin/email_templates_admin_ui.js", nil))
	if jsRec.Code != http.StatusOK {
		t.Fatalf("email_templates_admin_ui.js status = %d", jsRec.Code)
	}
	body := jsRec.Body.String()
	for _, expected := range []string{
		"ShopandaEmailTemplatesAdminUI",
		"escAttr",
		"newLoadGate",
		"createScopedEditorSession",
		"canShowWriteControls",
		"listLoadResult",
		"applyRowToFields",
		"formMatchesBaseline",
		"editorShellHTML",
		"normalizeLocaleInput",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in email_templates_admin_ui.js", expected)
		}
	}

	adminJS := httptest.NewRecorder()
	h.ServeHTTP(adminJS, httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil))
	if adminJS.Code != http.StatusOK {
		t.Fatalf("admin.js status = %d", adminJS.Code)
	}
	adminBody := adminJS.Body.String()
	if !strings.Contains(adminBody, "content.write") {
		t.Fatal("adminRoleDefinitions must include content.write")
	}
	if !strings.Contains(adminBody, "/admin/content/email-templates/cart_recovery") {
		t.Fatal("abandoned-cart hint must link to cart_recovery editor")
	}
}

func TestEmailTemplatesAdminUI_Node(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for email template UI tests (Node 22+; see docs/guides/DEVELOPER.md): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", "email_templates_admin_ui_test.js")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test email_templates_admin_ui_test.js: %v\n%s", err, out)
	}
}
