package runtime

import (
	"time"

	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ShutdownBackground cancels background contexts, stops the scheduler, and waits
// for goroutines to finish up to timeout.
func ShutdownBackground(log logger.Logger, timeout time.Duration, sched scheduler.Scheduler, cancels []func(), dones []<-chan struct{}) {
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
	if sched != nil {
		sched.Stop()
	}
	deadline := time.After(timeout)
	for _, done := range dones {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-deadline:
			log.Info("background.shutdown.timeout", nil)
			return
		}
	}
}
