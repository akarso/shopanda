package integrationhttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/pkg/integrationhttp"
)

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	integrationhttp.WriteError(rec, http.StatusBadRequest, "invalid_payload", "missing order_id", map[string]interface{}{"field": "order_id"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body["error"] != "invalid_payload" || body["message"] != "missing order_id" {
		t.Fatalf("body = %v", body)
	}
	details, ok := body["details"].(map[string]interface{})
	if !ok || details["field"] != "order_id" {
		t.Fatalf("details = %v", body["details"])
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	integrationhttp.WriteJSON(rec, http.StatusOK, map[string]string{"status": "accepted"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"accepted"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestDecodeJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"order_id":"100"}`))
	var payload struct {
		OrderID string `json:"order_id"`
	}
	if err := integrationhttp.DecodeJSON(req, 1024, &payload); err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if payload.OrderID != "100" {
		t.Fatalf("payload = %+v", payload)
	}
}
