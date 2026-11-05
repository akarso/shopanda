package extapi

import "errors"

var (
	errSyncJobNameRequired = errors.New("sync job name is required")
	errSyncJobNameInvalid  = errors.New("sync job name must not contain spaces")
	errSyncJobNotFound     = errors.New("sync job not registered")
	errSyncJobQueueUnbound = errors.New("sync job queue not bound")
)

// ErrSyncJobNameRequired indicates a sync job registration omitted Name.
var ErrSyncJobNameRequired = errSyncJobNameRequired

// ErrSyncJobNameInvalid indicates a sync job name is malformed.
var ErrSyncJobNameInvalid = errSyncJobNameInvalid

// ErrSyncJobNotFound indicates Enqueue named a job that was not registered
// for this integration slug.
var ErrSyncJobNotFound = errSyncJobNotFound

// ErrSyncJobQueueUnbound indicates Enqueue ran before the composition root
// called BindQueue (distinct from a plugin RegisterQueue during Init).
var ErrSyncJobQueueUnbound = errSyncJobQueueUnbound
