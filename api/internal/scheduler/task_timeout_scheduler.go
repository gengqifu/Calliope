package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/calliope/api/internal/service"
)

// StartTaskTimeoutScheduler launches a background goroutine that periodically
// calls FixTimedOutTasks. It stops when ctx is cancelled.
func StartTaskTimeoutScheduler(ctx context.Context, svc service.TaskService, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := svc.FixTimedOutTasks(ctx); err != nil {
					log.Printf("[scheduler] FixTimedOutTasks: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
