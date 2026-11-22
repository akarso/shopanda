package pluginsdk_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func TestValidationError_ClientCodeAndDetails(t *testing.T) {
	err := pluginsdk.ValidationError("design no longer sold",
		pluginsdk.WithClientCode("plugin.design_unavailable"),
		pluginsdk.WithDetails(map[string]interface{}{
			"lines":     []interface{}{map[string]interface{}{"variant_id": "var-1"}},
			"retryable": false,
		}),
	)
	rec := httptest.NewRecorder()
	pluginsdk.JSONError(rec, err)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var body struct {
		Error *struct {
			Code    string                 `json:"code"`
			Message string                 `json:"message"`
			Details map[string]interface{} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == nil || body.Error.Code != "plugin.design_unavailable" {
		t.Fatalf("error = %+v", body.Error)
	}
	if body.Error.Details["retryable"] != false {
		t.Fatalf("details = %#v", body.Error.Details)
	}
}

func TestUnavailableError_503(t *testing.T) {
	err := pluginsdk.UnavailableError("cart port down", pluginsdk.WithClientCode("plugin.cart_unavailable"))
	if !pluginsdk.IsUnavailable(err) {
		t.Fatal("IsUnavailable = false")
	}
	rec := httptest.NewRecorder()
	pluginsdk.JSONError(rec, err)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
