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
	// ErrPurgeDefinitionRestored indicates a purge batch found the field row active
	// again (e.g. Save restored it). Do not treat this as a purge retry.
	ErrPurgeDefinitionRestored = errors.New("value purge stopped: field definition is active again")
	// ErrPurgeDefinitionMissing indicates a purge batch found no extension_fields row.
	// Leftover extension_values may remain; this is not a soft-delete retry signal.
	ErrPurgeDefinitionMissing = errors.New("value purge stopped: field definition no longer exists")
)
