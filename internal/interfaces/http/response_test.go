package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestJSON(t *testing.T) {
	w := httptest.NewRecorder()
	shophttp.JSON(w, http.StatusOK, map[string]string{"id": "123"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp shophttp.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Error("error should be nil")
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data type = %T, want map", resp.Data)
	}
	if data["id"] != "123" {
		t.Errorf("data[id] = %v, want 123", data["id"])
	}
}

func TestJSONError_AppError(t *testing.T) {
	w := httptest.NewRecorder()
	shophttp.JSONError(w, apperror.NotFound("product not found"))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var resp shophttp.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data != nil {
		t.Error("data should be nil")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want not_found", resp.Error.Code)
	}
	if resp.Error.Message != "product not found" {
		t.Errorf("error.message = %q, want 'product not found'", resp.Error.Message)
	}
}

func TestJSONError_GenericError(t *testing.T) {
	w := httptest.NewRecorder()
	shophttp.JSONError(w, fmt.Errorf("something broke"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	var resp shophttp.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "internal" {
		t.Errorf("error.code = %q, want internal", resp.Error.Code)
	}
	if resp.Error.Message != "internal server error" {
		t.Errorf("error.message = %q, want 'internal server error'", resp.Error.Message)
	}
}

func TestStatusFromCode(t *testing.T) {
	tests := []struct {
		code   apperror.Code
		status int
	}{
		{apperror.CodeNotFound, http.StatusNotFound},
		{apperror.CodeConflict, http.StatusConflict},
		{apperror.CodeValidation, http.StatusUnprocessableEntity},
		{apperror.CodeUnauthorized, http.StatusUnauthorized},
		{apperror.CodeForbidden, http.StatusForbidden},
		{apperror.CodeInternal, http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(string(tc.code), func(t *testing.T) {
			if got := shophttp.StatusFromCode(tc.code); got != tc.status {
				t.Errorf("StatusFromCode(%q) = %d, want %d", tc.code, got, tc.status)
			}
		})
	}
}
