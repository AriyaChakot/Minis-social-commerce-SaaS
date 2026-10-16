// Package leader elects one leader among several copies of a process, so that periodic jobs run
// in one place at a time even when the worker is deployed with more than one replica.
//
// It uses a PostgreSQL session-level advisory lock: pg_try_advisory_lock(key) succeeds for exactly
// one database session at a time, and Postgres releases the lock automatically when that session
// ends, including when the process crashes or its network connection dies. No extra
// infrastructure (etcd, ZooKeeper, Redis) is needed.
//
// Leadership is a best-effort optimisation, not a correctness guarantee: if the leader's
// connection breaks, it notices only at its next check, so for a moment two processes may both
// believe they lead. The jobs must therefore be safe to run concurrently anyway (ours are: they
// lock rows and re-check state). The lock prevents duplicate work; the row locks prevent damage.
package leader

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Key turns a readable name into the 64-bit number an advisory lock is identified by.
func Key(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return int64(h.Sum64())
}

// Elector competes for one advisory lock.
type Elector struct {
	connConfig *pgx.ConnConfig
	key        int64

	RetryEvery time.Duration // how often a follower tries to become the leader
	CheckEvery time.Duration // how often the leader checks that its session (and lock) is alive
	Logf       func(format string, args ...any)
}

// New returns an Elector with default timings. connConfig is used for a dedicated connection
// (not a pooled one: the lock belongs to the session, and a pool hands connections around).
func New(connConfig *pgx.ConnConfig, key int64) *Elector {
	return &Elector{
		connConfig: connConfig,
		key:        key,
		RetryEvery: 15 * time.Second,
		CheckEvery: 5 * time.Second,
		Logf:       func(string, ...any) {},
	}
}

// Run blocks until ctx is cancelled. Each time this process becomes the leader, Run calls lead
// with a context that is cancelled as soon as leadership is lost or ctx ends; lead should return
// promptly when that happens. If lead returns on its own, the lock is released and Run competes
// again after RetryEvery.
func (e *Elector) Run(ctx context.Context, lead func(ctx context.Context)) {
	for {
		if err := e.term(ctx, lead); err != nil && ctx.Err() == nil {
			e.Logf("leader: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.RetryEvery):
		}
	}
}

// term is one attempt: connect, try the lock, and if it is ours, lead until something ends it.
func (e *Elector) term(ctx context.Context, lead func(ctx context.Context)) error {
	conn, err := pgx.ConnectConfig(ctx, e.connConfig)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	// Closing the session is what releases the lock, on every path out of this function.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, e.key).Scan(&acquired); err != nil {
		return fmt.Errorf("try lock: %w", err)
	}
	if !acquired {
		return nil // someone else leads; try again later
	}
	e.Logf("leader: became the leader")

	leadCtx, stopLeading := context.WithCancel(ctx)
	defer stopLeading()
	done := make(chan struct{})
	go func() {
		defer close(done)
		lead(leadCtx)
	}()

	check := time.NewTicker(e.CheckEvery)
	defer check.Stop()
	for {
		select {
		case <-done:
			e.Logf("leader: stepping down (work finished)")
			return nil
		case <-ctx.Done():
			stopLeading()
			<-done // let the current job reach a safe point before the lock is released
			e.Logf("leader: stepping down (shutdown)")
			return nil
		case <-check.C:
			pingCtx, cancel := context.WithTimeout(ctx, e.CheckEvery)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				// Our session may be gone, and with it the lock: another process may lead now.
				stopLeading()
				<-done
				return fmt.Errorf("lost leadership: %w", err)
			}
		}
	}
}
