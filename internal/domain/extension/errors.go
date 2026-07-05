package extension

import (
	"errors"
	"fmt"
)

// ValidationError marks domain validation failures for extension field definitions.
type ValidationError struct {
	msg string
}

func (e *ValidationError) Error() string {
	return e.msg
}

// ValidationErr returns a validation error with message msg.
func ValidationErr(msg string) error {
	return &ValidationError{msg: msg}
}

// ValidationErrf returns a formatted validation error.
func ValidationErrf(format string, args ...interface{}) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// IsValidationError reports whether err is an extension field validation error.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
