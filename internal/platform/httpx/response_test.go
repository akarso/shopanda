package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/httpx"
)

func TestJSONError_ValidationClientCodeAndDetails(t *testing.T) {
	err := apperror.Validation("design no longer sold").
		WithClientCode("plugin.design_unavailable").
		WithDetails(map[string]interface{}{
			"lines": []interface{}{
				map[string]interface{}{"variant_id": "var-1"},
				map[string]interface{}{"variant_id": "var-2"},
			},
			"retryable": false,
		})
	rec := httptest.NewRecorder()
	httpx.JSONError(rec, err)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var body httpx.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == nil || body.Error.Code != "plugin.design_unavailable" {
		t.Fatalf("error = %+v, want plugin.design_unavailable", body.Error)
	}
	if body.Error.Message != "design no longer sold" {
		t.Fatalf("message = %q", body.Error.Message)
	}
	lines, ok := body.Error.Details["lines"].([]interface{})
	if !ok || len(lines) != 2 {
		t.Fatalf("details.lines = %#v, want 2 entries", body.Error.Details["lines"])
	}
	if body.Error.Details["retryable"] != false {
		t.Fatalf("retryable = %#v", body.Error.Details["retryable"])
	}
}

func TestJSONError_Unavailable503(t *testing.T) {
	err := apperror.Unavailable("cart port down").WithClientCode("plugin.cart_unavailable")
	rec := httptest.NewRecorder()
	httpx.JSONError(rec, err)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body httpx.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == nil || body.Error.Code != "plugin.cart_unavailable" {
		t.Fatalf("error = %+v", body.Error)
	}
	if body.Error.Message != "cart port down" {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

func TestJSONError_InternalStillMasked(t *testing.T) {
	err := apperror.Internal("db exploded").WithClientCode("plugin.should_not_leak").
		WithDetails(map[string]interface{}{"secret": true})
	rec := httptest.NewRecorder()
	httpx.JSONError(rec, err)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var body httpx.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "internal" || body.Error.Message != "internal server error" {
		t.Fatalf("error = %+v", body.Error)
	}
	if body.Error.Details != nil {
		t.Fatalf("details must be masked, got %#v", body.Error.Details)
	}
}

func TestStatusFromCode_Unavailable(t *testing.T) {
	if got := httpx.StatusFromCode(apperror.CodeUnavailable); got != http.StatusServiceUnavailable {
		t.Fatalf("got %d", got)
	}
}
