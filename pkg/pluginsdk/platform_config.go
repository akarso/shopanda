package pluginsdk

import (
	"fmt"
	"strings"
)

// RequireQueueDriver fails closed when Host.PlatformConfig("queue.driver") is
// not one of the allowed values (empty driver is treated as "postgres", matching
// composition-root defaults). Use from Init when a plugin only supports a
// subset of queue backends.
func RequireQueueDriver(h Host, allowed ...string) error {
	if h == nil {
		return fmt.Errorf("pluginsdk: RequireQueueDriver: host is nil")
	}
	if len(allowed) == 0 {
		return fmt.Errorf("pluginsdk: RequireQueueDriver: at least one allowed driver is required")
	}
	got := strings.TrimSpace(h.PlatformConfig("queue.driver"))
	if got == "" {
		got = "postgres"
	}
	for _, a := range allowed {
		if got == a {
			return nil
		}
	}
	return fmt.Errorf("pluginsdk: queue.driver=%q is not supported (allowed: %s)", got, strings.Join(allowed, ", "))
}
