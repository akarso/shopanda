package runtime

import (
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ShutdownBackground cancels background contexts, stops the scheduler, and waits
// for goroutines to finish up to timeout. All done channels are waited in
// parallel so one slow component cannot starve the rest of the budget.
func ShutdownBackground(log logger.Logger, timeout time.Duration, sched scheduler.Scheduler, cancels []func(), dones []<-chan struct{}) {
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
	if sched != nil {
		sched.Stop()
	}

	var wg sync.WaitGroup
	for _, done := range dones {
		if done == nil {
			continue
		}
		wg.Add(1)
		go func(d <-chan struct{}) {
			defer wg.Done()
			<-d
		}(done)
	}
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(timeout):
		log.Info("background.shutdown.timeout", nil)
	}
}
