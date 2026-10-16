// Package worker runs the periodic background jobs: expiring unpaid storefront orders and
// deleting expired sessions. Only the elected leader runs them (see package leader).
package worker

import (
	"context"
	"log"
	"time"

	"mscs/internal/auth"
	"mscs/internal/order"
)

// Job is one periodic task. Run returns how many items it handled (for the log).
type Job struct {
	Name string
	Run  func(ctx context.Context) (int64, error)
}

// Jobs builds the worker's job list. An order is expired when it is still unpaid paymentWindow
// after it was placed; now is replaceable in tests.
func Jobs(orders *order.Service, sessions *auth.Service, paymentWindow time.Duration, now func() time.Time) []Job {
	return []Job{
		{
			Name: "expire-unpaid-orders",
			Run: func(ctx context.Context) (int64, error) {
				n, err := orders.ExpireUnpaid(ctx, now().Add(-paymentWindow))
				return int64(n), err
			},
		},
		{
			Name: "delete-expired-sessions",
			Run:  sessions.DeleteExpiredSessions,
		},
	}
}

// Loop runs every job right away and then once per interval, until ctx is cancelled.
// A job's failure is logged and retried on the next tick; it never stops the other jobs.
func Loop(ctx context.Context, interval time.Duration, jobs []Job) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		RunOnce(ctx, interval, jobs)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce runs each job once, giving each at most timeout. It returns early if ctx is cancelled.
func RunOnce(ctx context.Context, timeout time.Duration, jobs []Job) {
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		jobCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		n, err := job.Run(jobCtx)
		cancel()
		switch {
		case err != nil:
			log.Printf("job %s: handled %d, error: %v", job.Name, n, err)
		case n > 0: // stay quiet when there was nothing to do
			log.Printf("job %s: handled %d in %s", job.Name, n, time.Since(start).Round(time.Millisecond))
		}
	}
}
