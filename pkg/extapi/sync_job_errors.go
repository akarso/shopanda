package extapi

import "errors"

var (
	errSyncJobNameRequired = errors.New("sync job name is required")
	errSyncJobNameInvalid  = errors.New("sync job name must not contain spaces")
)

// ErrSyncJobNameRequired indicates a sync job registration omitted Name.
var ErrSyncJobNameRequired = errSyncJobNameRequired

// ErrSyncJobNameInvalid indicates a sync job name is malformed.
var ErrSyncJobNameInvalid = errSyncJobNameInvalid
