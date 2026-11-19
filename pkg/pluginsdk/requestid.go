package pluginsdk

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/requestctx"
)

// RequestIDFromContext returns the request id stored on ctx by HTTP middleware
// or sync-job reinjection, or "" when absent. Public read accessor so plugins
// correlate logs without importing internal/platform/requestctx.
func RequestIDFromContext(ctx context.Context) string {
	return requestctx.RequestID(ctx)
}

// InjectRequestID shallow-copies payload and, when ctx carries a request ID
// and the payload has no usable request_id, stores it as "request_id".
// SyncJobs.Enqueue already does this — call only when building a payload
// outside Enqueue. Same behavior as the former plugin.InjectRequestID.
func InjectRequestID(ctx context.Context, payload map[string]interface{}) map[string]interface{} {
	return plugin.InjectRequestID(ctx, payload)
}
