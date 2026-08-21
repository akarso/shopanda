package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
)

func TestExtensionHookAdminHandler_ListHooks(t *testing.T) {
	reg := hooksapp.NewRegistry(nil)
	if err := reg.Register(hooksapp.HookCartAddItemAfter, 100, "plugin.a", func(ctx *hooksapp.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register(hooksapp.HookCartAddItemAfter, 200, "plugin.b", func(ctx *hooksapp.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	h := admin.NewExtensionHookAdminHandler(reg)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/extensions/hooks", h.ListHooks())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/extensions/hooks", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	hooks := data["hooks"].([]interface{})
	if len(hooks) != 1 {
		t.Fatalf("hooks = %v", hooks)
	}
	entry := hooks[0].(map[string]interface{})
	if entry["name"] != hooksapp.HookCartAddItemAfter {
		t.Fatalf("name = %v", entry["name"])
	}
	handlers := entry["handlers"].([]interface{})
	if len(handlers) != 2 {
		t.Fatalf("handlers = %v", handlers)
	}
}

func TestExtensionHookAdminHandler_NilRegistryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil hook registry")
		}
	}()
	admin.NewExtensionHookAdminHandler(nil)
}
