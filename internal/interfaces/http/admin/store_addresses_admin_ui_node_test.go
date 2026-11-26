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

func TestAdminHandler_StoreAddressesAdminUIScript(t *testing.T) {
	h := newAdminHandler(t)

	indexRec := httptest.NewRecorder()
	h.ServeHTTP(indexRec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d", indexRec.Code)
	}
	index := indexRec.Body.String()
	uiPos := strings.Index(index, `src="/admin/store_addresses_admin_ui.js"`)
	adminPos := strings.Index(index, `src="/admin/admin.js"`)
	navPos := strings.Index(index, `href="/admin/settings/store-addresses"`)
	if uiPos < 0 || adminPos < 0 || uiPos > adminPos {
		t.Fatal("index.html must load store_addresses_admin_ui.js before admin.js")
	}
	if navPos < 0 {
		t.Fatal("index.html must include Store Addresses nav link")
	}

	jsRec := httptest.NewRecorder()
	h.ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/admin/store_addresses_admin_ui.js", nil))
	if jsRec.Code != http.StatusOK {
		t.Fatalf("store_addresses_admin_ui.js status = %d", jsRec.Code)
	}
	body := jsRec.Body.String()
	for _, expected := range []string{
		"ShopandaStoreAddressesAdminUI",
		"newStoreAddressesLoadGate",
		"applyAddressValues",
		"fallbackHintForRole",
		"buildAddressPutBody",
		"addressIsEmpty",
		"shouldMutateAddressRolePage",
		"shouldRefetchAddressRoleAfterWrite",
		"shouldApplyAddressFill",
		"newAddressFillGate",
		"addressRefetchPaintAction",
		"shouldCommitPageLoadResult",
		"shouldShowAddressWriteFeedback",
		"fieldMaxLengths",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in store_addresses_admin_ui.js", expected)
		}
	}
}

func TestStoreAddressesAdminUI_Node(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for store addresses UI tests (Node 22+; see docs/guides/DEVELOPER.md): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", "store_addresses_admin_ui_test.js")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test store_addresses_admin_ui_test.js: %v\n%s", err, out)
	}
}
