#!/usr/bin/env python3
"""Create Go files for PR-008: Error handling foundation."""
import os

BASE = "/Users/akarso/_sites/projects/shopanda"

files = {}

# --- internal/platform/apperror/apperror.go ---
files["internal/platform/apperror/apperror.go"] = """\
package apperror

import (
\t"errors"
\t"fmt"
)

// Code represents a category of application error.
type Code string

const (
\tCodeNotFound     Code = "not_found"
\tCodeConflict     Code = "conflict"
\tCodeValidation   Code = "validation"
\tCodeUnauthorized Code = "unauthorized"
\tCodeForbidden    Code = "forbidden"
\tCodeInternal     Code = "internal"
)

// Error is a structured application error.
type Error struct {
\tCode    Code
\tMessage string
\tErr     error
}

func (e *Error) Error() string {
\tif e.Err != nil {
\t\treturn fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
\t}
\treturn fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
\treturn e.Err
}

// New creates a new Error with the given code and message.
func New(code Code, message string) *Error {
\treturn &Error{Code: code, Message: message}
}

// Wrap creates a new Error wrapping an existing error.
func Wrap(code Code, message string, err error) *Error {
\treturn &Error{Code: code, Message: message, Err: err}
}

// NotFound creates a not_found error.
func NotFound(message string) *Error {
\treturn New(CodeNotFound, message)
}

// Conflict creates a conflict error.
func Conflict(message string) *Error {
\treturn New(CodeConflict, message)
}

// Validation creates a validation error.
func Validation(message string) *Error {
\treturn New(CodeValidation, message)
}

// Unauthorized creates an unauthorized error.
func Unauthorized(message string) *Error {
\treturn New(CodeUnauthorized, message)
}

// Forbidden creates a forbidden error.
func Forbidden(message string) *Error {
\treturn New(CodeForbidden, message)
}

// Internal creates an internal error.
func Internal(message string) *Error {
\treturn New(CodeInternal, message)
}

// Is checks whether err (or any wrapped error) is an *Error with the given code.
func Is(err error, code Code) bool {
\tvar appErr *Error
\tif errors.As(err, &appErr) {
\t\treturn appErr.Code == code
\t}
\treturn false
}
"""

# --- internal/platform/apperror/apperror_test.go ---
files["internal/platform/apperror/apperror_test.go"] = """\
package apperror_test

import (
\t"errors"
\t"fmt"
\t"testing"

\t"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestError_Error(t *testing.T) {
\terr := apperror.New(apperror.CodeNotFound, "product not found")
\twant := "not_found: product not found"
\tif got := err.Error(); got != want {
\t\tt.Errorf("Error() = %q, want %q", got, want)
\t}
}

func TestError_ErrorWithWrap(t *testing.T) {
\tinner := fmt.Errorf("db: no rows")
\terr := apperror.Wrap(apperror.CodeNotFound, "product not found", inner)
\twant := "not_found: product not found: db: no rows"
\tif got := err.Error(); got != want {
\t\tt.Errorf("Error() = %q, want %q", got, want)
\t}
}

func TestError_Unwrap(t *testing.T) {
\tinner := fmt.Errorf("db: no rows")
\terr := apperror.Wrap(apperror.CodeInternal, "query failed", inner)
\tif !errors.Is(err, inner) {
\t\tt.Error("Unwrap should return the inner error")
\t}
}

func TestIs(t *testing.T) {
\terr := apperror.NotFound("item not found")
\tif !apperror.Is(err, apperror.CodeNotFound) {
\t\tt.Error("Is should match CodeNotFound")
\t}
\tif apperror.Is(err, apperror.CodeConflict) {
\t\tt.Error("Is should not match CodeConflict")
\t}
}

func TestIs_Wrapped(t *testing.T) {
\tinner := apperror.NotFound("missing")
\twrapped := fmt.Errorf("service: %w", inner)
\tif !apperror.Is(wrapped, apperror.CodeNotFound) {
\t\tt.Error("Is should match through wrapping")
\t}
}

func TestIs_NonAppError(t *testing.T) {
\terr := fmt.Errorf("plain error")
\tif apperror.Is(err, apperror.CodeNotFound) {
\t\tt.Error("Is should return false for non-AppError")
\t}
}

func TestConstructors(t *testing.T) {
\ttests := []struct {
\t\tname string
\t\terr  *apperror.Error
\t\tcode apperror.Code
\t}{
\t\t{"NotFound", apperror.NotFound("x"), apperror.CodeNotFound},
\t\t{"Conflict", apperror.Conflict("x"), apperror.CodeConflict},
\t\t{"Validation", apperror.Validation("x"), apperror.CodeValidation},
\t\t{"Unauthorized", apperror.Unauthorized("x"), apperror.CodeUnauthorized},
\t\t{"Forbidden", apperror.Forbidden("x"), apperror.CodeForbidden},
\t\t{"Internal", apperror.Internal("x"), apperror.CodeInternal},
\t}
\tfor _, tc := range tests {
\t\tt.Run(tc.name, func(t *testing.T) {
\t\t\tif tc.err.Code != tc.code {
\t\t\t\tt.Errorf("got code %q, want %q", tc.err.Code, tc.code)
\t\t\t}
\t\t})
\t}
}
"""

# --- internal/interfaces/http/response.go ---
files["internal/interfaces/http/response.go"] = """\
package http

import (
\t"encoding/json"
\t"errors"
\t"net/http"

\t"github.com/akarso/shopanda/internal/platform/apperror"
)

// Response is the standard API response envelope.
type Response struct {
\tData  interface{} `json:"data"`
\tError *ErrorBody  `json:"error"`
}

// ErrorBody represents the error portion of an API response.
type ErrorBody struct {
\tCode    string `json:"code"`
\tMessage string `json:"message"`
}

// JSON writes a JSON response with the given status code and data.
func JSON(w http.ResponseWriter, status int, data interface{}) {
\tw.Header().Set("Content-Type", "application/json")
\tw.WriteHeader(status)
\tjson.NewEncoder(w).Encode(Response{Data: data})
}

// JSONError writes a JSON error response derived from err.
// If err is an *apperror.Error, the code and message are taken from it.
// Otherwise a generic 500 response is returned.
func JSONError(w http.ResponseWriter, err error) {
\tvar appErr *apperror.Error
\tif errors.As(err, &appErr) {
\t\tstatus := StatusFromCode(appErr.Code)
\t\tw.Header().Set("Content-Type", "application/json")
\t\tw.WriteHeader(status)
\t\tjson.NewEncoder(w).Encode(Response{
\t\t\tError: &ErrorBody{
\t\t\t\tCode:    string(appErr.Code),
\t\t\t\tMessage: appErr.Message,
\t\t\t},
\t\t})
\t\treturn
\t}

\tw.Header().Set("Content-Type", "application/json")
\tw.WriteHeader(http.StatusInternalServerError)
\tjson.NewEncoder(w).Encode(Response{
\t\tError: &ErrorBody{
\t\t\tCode:    string(apperror.CodeInternal),
\t\t\tMessage: "internal server error",
\t\t},
\t})
}

// StatusFromCode maps an apperror.Code to an HTTP status code.
func StatusFromCode(code apperror.Code) int {
\tswitch code {
\tcase apperror.CodeNotFound:
\t\treturn http.StatusNotFound
\tcase apperror.CodeConflict:
\t\treturn http.StatusConflict
\tcase apperror.CodeValidation:
\t\treturn http.StatusUnprocessableEntity
\tcase apperror.CodeUnauthorized:
\t\treturn http.StatusUnauthorized
\tcase apperror.CodeForbidden:
\t\treturn http.StatusForbidden
\tdefault:
\t\treturn http.StatusInternalServerError
\t}
}
"""

# --- internal/interfaces/http/response_test.go ---
files["internal/interfaces/http/response_test.go"] = """\
package http_test

import (
\t"encoding/json"
\t"fmt"
\t"net/http"
\t"net/http/httptest"
\t"testing"

\tshophttp "github.com/akarso/shopanda/internal/interfaces/http"
\t"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestJSON(t *testing.T) {
\tw := httptest.NewRecorder()
\tshophttp.JSON(w, http.StatusOK, map[string]string{"id": "123"})

\tif w.Code != http.StatusOK {
\t\tt.Errorf("status = %d, want %d", w.Code, http.StatusOK)
\t}
\tif ct := w.Header().Get("Content-Type"); ct != "application/json" {
\t\tt.Errorf("Content-Type = %q, want application/json", ct)
\t}

\tvar resp shophttp.Response
\tif err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
\t\tt.Fatalf("unmarshal: %v", err)
\t}
\tif resp.Error != nil {
\t\tt.Error("error should be nil")
\t}
\tdata, ok := resp.Data.(map[string]interface{})
\tif !ok {
\t\tt.Fatalf("data type = %T, want map", resp.Data)
\t}
\tif data["id"] != "123" {
\t\tt.Errorf("data[id] = %v, want 123", data["id"])
\t}
}

func TestJSONError_AppError(t *testing.T) {
\tw := httptest.NewRecorder()
\tshophttp.JSONError(w, apperror.NotFound("product not found"))

\tif w.Code != http.StatusNotFound {
\t\tt.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
\t}

\tvar resp shophttp.Response
\tif err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
\t\tt.Fatalf("unmarshal: %v", err)
\t}
\tif resp.Data != nil {
\t\tt.Error("data should be nil")
\t}
\tif resp.Error == nil {
\t\tt.Fatal("error should not be nil")
\t}
\tif resp.Error.Code != "not_found" {
\t\tt.Errorf("error.code = %q, want not_found", resp.Error.Code)
\t}
\tif resp.Error.Message != "product not found" {
\t\tt.Errorf("error.message = %q, want 'product not found'", resp.Error.Message)
\t}
}

func TestJSONError_GenericError(t *testing.T) {
\tw := httptest.NewRecorder()
\tshophttp.JSONError(w, fmt.Errorf("something broke"))

\tif w.Code != http.StatusInternalServerError {
\t\tt.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
\t}

\tvar resp shophttp.Response
\tif err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
\t\tt.Fatalf("unmarshal: %v", err)
\t}
\tif resp.Error == nil {
\t\tt.Fatal("error should not be nil")
\t}
\tif resp.Error.Code != "internal" {
\t\tt.Errorf("error.code = %q, want internal", resp.Error.Code)
\t}
\tif resp.Error.Message != "internal server error" {
\t\tt.Errorf("error.message = %q, want 'internal server error'", resp.Error.Message)
\t}
}

func TestStatusFromCode(t *testing.T) {
\ttests := []struct {
\t\tcode   apperror.Code
\t\tstatus int
\t}{
\t\t{apperror.CodeNotFound, http.StatusNotFound},
\t\t{apperror.CodeConflict, http.StatusConflict},
\t\t{apperror.CodeValidation, http.StatusUnprocessableEntity},
\t\t{apperror.CodeUnauthorized, http.StatusUnauthorized},
\t\t{apperror.CodeForbidden, http.StatusForbidden},
\t\t{apperror.CodeInternal, http.StatusInternalServerError},
\t}
\tfor _, tc := range tests {
\t\tt.Run(string(tc.code), func(t *testing.T) {
\t\t\tif got := shophttp.StatusFromCode(tc.code); got != tc.status {
\t\t\t\tt.Errorf("StatusFromCode(%q) = %d, want %d", tc.code, got, tc.status)
\t\t\t}
\t\t})
\t}
}
"""

for path, content in files.items():
    full = os.path.join(BASE, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w") as f:
        f.write(content)

print("created", len(files), "files")
for p in sorted(files):
    print(" ", p)
