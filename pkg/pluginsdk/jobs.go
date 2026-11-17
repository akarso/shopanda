package pluginsdk

import "github.com/akarso/shopanda/internal/domain/jobs"

// ErrSkipOutcome tells the worker to skip platform Complete/Fail. Prefer
// returning this alone from a sync/job handler (or fence wrapper). Skipping
// without a fence Complete/Fail/Requeue leaves the row in processing.
// Same sentinel as jobs.ErrSkipOutcome.
var ErrSkipOutcome = jobs.ErrSkipOutcome
