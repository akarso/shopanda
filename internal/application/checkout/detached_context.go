package checkout

import (
	"context"
	"time"
)

// compensateTimeout bounds compensating writes (rollback / post-side-effect persistence)
// that must complete even when the request context is already canceled.
const compensateTimeout = 30 * time.Second

// detachedTimeout returns a context that does not inherit parent cancellation/deadline,
// with its own bounded timeout. Use for:
//   - compensating rollback (release reservations, re-issue store credit)
//   - durably recording outcomes after a side effect already committed
//     (PSP capture, order save + item extension snapshots)
func detachedTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}
