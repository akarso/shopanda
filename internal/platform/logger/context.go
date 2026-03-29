package logger

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/requestctx"
)

// ContextLogger wraps a Logger and automatically includes request_id from context.
type ContextLogger struct {
	inner Logger
	ctx   context.Context
}

// WithContext returns a ContextLogger that extracts correlation IDs from ctx.
func WithContext(log Logger, ctx context.Context) *ContextLogger {
	return &ContextLogger{inner: log, ctx: ctx}
}

func (cl *ContextLogger) Info(event string, ctx map[string]interface{}) {
	cl.inner.Info(event, cl.merge(ctx))
}

func (cl *ContextLogger) Warn(event string, ctx map[string]interface{}) {
	cl.inner.Warn(event, cl.merge(ctx))
}

func (cl *ContextLogger) Error(event string, err error, ctx map[string]interface{}) {
	cl.inner.Error(event, err, cl.merge(ctx))
}

func (cl *ContextLogger) merge(ctx map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(ctx)+1)
	for k, v := range ctx {
		merged[k] = v
	}
	if id := requestctx.RequestID(cl.ctx); id != "" {
		merged["request_id"] = id
	}
	return merged
}
