package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	portsapp "github.com/akarso/shopanda/internal/application/ports"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestExtensionPortAdminHandler_ListPorts(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
		Cache:  config.CacheConfig{Driver: "postgres"},
		Queue:  config.QueueConfig{Driver: "postgres"},
		Media:  config.MediaConfig{Storage: "local"},
	}
	snapshot := portsapp.BuildSnapshot(&plugin.App{}, cfg)

	h := shophttp.NewExtensionPortAdminHandler(snapshot)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/extensions/ports", h.ListPorts())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/extensions/ports", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	ports := data["ports"].([]interface{})
	if len(ports) < 5 {
		t.Fatalf("ports = %d, want at least 5 catalog entries", len(ports))
	}

	first := ports[0].(map[string]interface{})
	if first["name"] != "search" {
		t.Fatalf("first port name = %v", first["name"])
	}
	if first["status"] != "core_default" {
		t.Fatalf("search status = %v", first["status"])
	}

	var tax map[string]interface{}
	for _, item := range ports {
		entry := item.(map[string]interface{})
		if entry["name"] == "tax" {
			tax = entry
			break
		}
	}
	if tax == nil {
		t.Fatal("tax port not found")
	}
	if tax["status"] != "core_default" {
		t.Fatalf("tax status = %v, want core_default", tax["status"])
	}
}
