package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	slotsapp "github.com/akarso/shopanda/internal/application/slots"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestExtensionSlotAdminHandler_ListSlots(t *testing.T) {
	reg := slotsapp.NewRegistry(nil)
	if err := reg.RegisterRenderer(string(extapi.SlotPDPInfo), slotsapp.PlacementAppend, 100, "plugin.a", func(ctx *slotsapp.RenderContext) string {
		return "<span>A</span>"
	}); err != nil {
		t.Fatalf("RegisterRenderer: %v", err)
	}
	if err := reg.RegisterRenderer(string(extapi.SlotPDPInfo), slotsapp.PlacementBefore, 200, "plugin.b", func(ctx *slotsapp.RenderContext) string {
		return "<span>B</span>"
	}); err != nil {
		t.Fatalf("RegisterRenderer: %v", err)
	}
	if err := reg.RegisterRenderer("custom.anchor", slotsapp.PlacementAppend, 50, "plugin.c", func(ctx *slotsapp.RenderContext) string {
		return "<span>C</span>"
	}); err != nil {
		t.Fatalf("RegisterRenderer: %v", err)
	}

	h := shophttp.NewExtensionSlotAdminHandler(reg)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/extensions/slots", h.ListSlots())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/extensions/slots", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	slots := data["slots"].([]interface{})
	if len(slots) != len(extapi.SlotAnchorNames())+1 {
		t.Fatalf("slots len = %d, want %d", len(slots), len(extapi.SlotAnchorNames())+1)
	}

	first := slots[0].(map[string]interface{})
	if first["name"] != string(extapi.SlotLayoutHead) {
		t.Fatalf("first slot name = %v", first["name"])
	}
	if first["group"] != slotsapp.GroupLayout {
		t.Fatalf("first slot group = %v", first["group"])
	}

	var pdpInfo map[string]interface{}
	for _, item := range slots {
		entry := item.(map[string]interface{})
		if entry["name"] == string(extapi.SlotPDPInfo) {
			pdpInfo = entry
			break
		}
	}
	if pdpInfo == nil {
		t.Fatal("missing pdp.info entry")
	}
	handlers := pdpInfo["handlers"].([]interface{})
	if len(handlers) != 2 {
		t.Fatalf("pdp.info handlers = %v", handlers)
	}

	last := slots[len(slots)-1].(map[string]interface{})
	if last["name"] != "custom.anchor" {
		t.Fatalf("last slot name = %v", last["name"])
	}
}

func TestExtensionSlotAdminHandler_NilRegistryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil slot registry")
		}
	}()
	shophttp.NewExtensionSlotAdminHandler(nil)
}

func TestExtensionSlotAdminHandler_UsesStableAnchorOrder(t *testing.T) {
	reg := slotsapp.NewRegistry(nil)
	h := shophttp.NewExtensionSlotAdminHandler(reg)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/extensions/slots", h.ListSlots())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/admin/extensions/slots", nil))

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	slots := body["data"].(map[string]interface{})["slots"].([]interface{})
	names := make([]string, len(slots))
	for i, item := range slots {
		names[i] = item.(map[string]interface{})["name"].(string)
	}
	if len(names) != len(extapi.SlotAnchorNames()) {
		t.Fatalf("names len = %d, want %d", len(names), len(extapi.SlotAnchorNames()))
	}
	for i, want := range extapi.SlotAnchorNames() {
		if names[i] != want {
			t.Fatalf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}
