package apperror

import (
	"errors"
	"fmt"
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
	CodeInternal     Code = "internal"
)

// Error is a structured application error.
type Error struct {
	Code    Code
	Message string
	Err     error
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

// RateLimited creates a rate_limited error.
func RateLimited(message string) *Error {
	return New(CodeRateLimited, message)
}

// Internal creates an internal error.
func Internal(message string) *Error {
	return New(CodeInternal, message)
}

// Is checks whether err (or any wrapped error) is an *Error with the given code.
func Is(err error, code Code) bool {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}
