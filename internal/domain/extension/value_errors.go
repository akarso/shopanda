package extension

import "errors"

var (
	// ErrUnknownFieldCode indicates the field code is not registered.
	ErrUnknownFieldCode = errors.New("unknown field code")
	// ErrForbiddenPrivateField indicates a private field write without capability.
	ErrForbiddenPrivateField = errors.New("forbidden private field")
)
