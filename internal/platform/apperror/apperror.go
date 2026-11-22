package apperror

import (
	"errors"
	"fmt"
	"strings"
)

// Code represents a category of application error.
type Code string

const (
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeValidation   Code = "validation"
	CodeUnauthorized Code = "unauthorized"
	CodeForbidden    Code = "forbidden"
	CodeRateLimited  Code = "rate_limited"
	CodeUnavailable  Code = "unavailable"
	CodeInternal     Code = "internal"

	CodeForbiddenPrivateField    Code = "forbidden_private_field"
	CodeForbiddenPluginOnlyField Code = "forbidden_plugin_only_field"
	CodePluginOnlyFieldImmutable Code = "plugin_only_field_immutable"
	CodeUnknownFieldCode         Code = "unknown_field_code"
	CodeFieldValidationFailed    Code = "field_validation_failed"
	CodeCartValidationFailed     Code = "cart_validation_failed"
)

// Error is a structured application error.
type Error struct {
	Code    Code
	Message string
	Err     error
	// ClientCode is an optional stable code for API clients (e.g. plugin.design_unavailable).
	// When set, JSONError uses it as the response error.code while StatusFromCode still
	// keys off Code (validation stays 422, unavailable stays 503).
	ClientCode string
	// Details is an optional structured payload (e.g. failing cart lines, retryable).
	Details map[string]interface{}
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// New creates a new Error with the given code and message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap creates a new Error wrapping an existing error.
func Wrap(code Code, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

// NotFound creates a not_found error.
func NotFound(message string) *Error {
	return New(CodeNotFound, message)
}

// Conflict creates a conflict error.
func Conflict(message string) *Error {
	return New(CodeConflict, message)
}

// Validation creates a validation error.
func Validation(message string) *Error {
	return New(CodeValidation, message)
}

// Unauthorized creates an unauthorized error.
func Unauthorized(message string) *Error {
	return New(CodeUnauthorized, message)
}

// Forbidden creates a forbidden error.
func Forbidden(message string) *Error {
	return New(CodeForbidden, message)
}

// ForbiddenPrivateField creates a forbidden_private_field error.
func ForbiddenPrivateField(message string) *Error {
	return New(CodeForbiddenPrivateField, message)
}

// ForbiddenPluginOnlyField creates a forbidden_plugin_only_field error.
func ForbiddenPluginOnlyField(message string) *Error {
	return New(CodeForbiddenPluginOnlyField, message)
}

// PluginOnlyFieldImmutable creates a plugin_only_field_immutable error.
func PluginOnlyFieldImmutable(message string) *Error {
	return New(CodePluginOnlyFieldImmutable, message)
}

// UnknownFieldCode creates an unknown_field_code error.
func UnknownFieldCode(message string) *Error {
	return New(CodeUnknownFieldCode, message)
}

// CartValidationFailed creates a cart_validation_failed error.
func CartValidationFailed(message string) *Error {
	return New(CodeCartValidationFailed, message)
}

// FieldValidationFailed creates a field_validation_failed error.
func FieldValidationFailed(message string) *Error {
	return New(CodeFieldValidationFailed, message)
}

// RateLimited creates a rate_limited error.
func RateLimited(message string) *Error {
	return New(CodeRateLimited, message)
}

// Unavailable creates an unavailable error (HTTP 503).
func Unavailable(message string) *Error {
	return New(CodeUnavailable, message)
}

// Internal creates an internal error.
func Internal(message string) *Error {
	return New(CodeInternal, message)
}

// WithClientCode sets a stable plugin-owned client-facing code and returns e.
// Codes must be "plugin." plus a non-empty slug (letters, digits, `_`, `-`, `.`).
// Invalid values are ignored so callers cannot override platform codes like
// "unauthorized" or "internal" on a 422/503 response.
func (e *Error) WithClientCode(code string) *Error {
	if e == nil {
		return nil
	}
	code = strings.TrimSpace(code)
	if !validPluginClientCode(code) {
		return e
	}
	e.ClientCode = code
	return e
}

func validPluginClientCode(code string) bool {
	const prefix = "plugin."
	if !strings.HasPrefix(code, prefix) {
		return false
	}
	slug := code[len(prefix):]
	if slug == "" {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}

// WithDetails sets an optional details payload and returns e.
// Nested map[string]interface{} and []interface{} values are deep-copied so
// later mutation by the caller cannot race the response encoder. Typed maps
// or slices (e.g. map[string]string) are not cloned — pass JSON-shaped trees.
func (e *Error) WithDetails(details map[string]interface{}) *Error {
	if e == nil {
		return nil
	}
	if details == nil {
		e.Details = nil
		return e
	}
	e.Details = cloneDetailsMap(details)
	return e
}

func cloneDetailsMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = cloneDetailsValue(v)
	}
	return out
}

func cloneDetailsValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		if t == nil {
			return map[string]interface{}(nil)
		}
		return cloneDetailsMap(t)
	case []interface{}:
		if t == nil {
			return []interface{}(nil)
		}
		out := make([]interface{}, len(t))
		for i, vv := range t {
			out[i] = cloneDetailsValue(vv)
		}
		return out
	default:
		return v
	}
}

// Is checks whether err (or any wrapped error) is an *Error with the given code.
func Is(err error, code Code) bool {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}
