package apperror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestError_Error(t *testing.T) {
	err := apperror.New(apperror.CodeNotFound, "product not found")
	want := "not_found: product not found"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_ErrorWithWrap(t *testing.T) {
	inner := fmt.Errorf("db: no rows")
	err := apperror.Wrap(apperror.CodeNotFound, "product not found", inner)
	want := "not_found: product not found: db: no rows"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("db: no rows")
	err := apperror.Wrap(apperror.CodeInternal, "query failed", inner)
	if !errors.Is(err, inner) {
		t.Error("Unwrap should return the inner error")
	}
}

func TestIs(t *testing.T) {
	err := apperror.NotFound("item not found")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Error("Is should match CodeNotFound")
	}
	if apperror.Is(err, apperror.CodeConflict) {
		t.Error("Is should not match CodeConflict")
	}
}

func TestIs_Wrapped(t *testing.T) {
	inner := apperror.NotFound("missing")
	wrapped := fmt.Errorf("service: %w", inner)
	if !apperror.Is(wrapped, apperror.CodeNotFound) {
		t.Error("Is should match through wrapping")
	}
}

func TestIs_NonAppError(t *testing.T) {
	err := fmt.Errorf("plain error")
	if apperror.Is(err, apperror.CodeNotFound) {
		t.Error("Is should return false for non-AppError")
	}
}

func TestConstructors(t *testing.T) {
	tests := []struct {
		name string
		err  *apperror.Error
		code apperror.Code
	}{
		{"NotFound", apperror.NotFound("x"), apperror.CodeNotFound},
		{"Conflict", apperror.Conflict("x"), apperror.CodeConflict},
		{"Validation", apperror.Validation("x"), apperror.CodeValidation},
		{"Unauthorized", apperror.Unauthorized("x"), apperror.CodeUnauthorized},
		{"Forbidden", apperror.Forbidden("x"), apperror.CodeForbidden},
		{"Internal", apperror.Internal("x"), apperror.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("got code %q, want %q", tc.err.Code, tc.code)
			}
		})
	}
}
