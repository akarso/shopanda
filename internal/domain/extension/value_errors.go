package extension

import "errors"

var (
	// ErrUnknownFieldCode indicates the field code is not registered.
	ErrUnknownFieldCode = errors.New("unknown field code")
	// ErrForbiddenPrivateField indicates a private field write without capability.
	ErrForbiddenPrivateField = errors.New("forbidden private field")
	// ErrForbiddenPluginOnlyField indicates a write to a plugin-only field from an untrusted path.
	ErrForbiddenPluginOnlyField = errors.New("field can only be written by a plugin")
	// ErrPurgeIncomplete indicates the plugin-only definition was soft-deleted but
	// deleting extension_values did not finish; retry with purge on the soft-deleted row.
	ErrPurgeIncomplete = errors.New("definition retired; value purge incomplete")
)
