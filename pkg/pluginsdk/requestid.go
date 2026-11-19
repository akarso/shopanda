package pluginsdk

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/requestctx"
)

// RequestIDFromContext returns the request id stored on ctx by HTTP middleware
// or sync-job reinjection, or "" when absent. Public read accessor so plugins
// correlate logs without importing internal/platform/requestctx.
func RequestIDFromContext(ctx context.Context) string {
	return requestctx.RequestID(ctx)
}
