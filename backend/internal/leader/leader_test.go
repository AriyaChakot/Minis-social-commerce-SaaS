package leader_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"mscs/internal/leader"
	"mscs/internal/testutil/dbtest"
)

// TestOnlyOneLeader starts two electors on the same lock: only one may lead, and when its
// process stops, the other one takes over.
func TestOnlyOneLeader(t *testing.T) {
	pool := dbtest.Pool(t)
	key := leader.Key("leader-test-" + dbtest.Suffix()) // never the real worker's lock

	var mu sync.Mutex
	var leading []string // names of the processes leading right now
	var history []string // every process that ever became the leader, in order
	maxAtOnce := 0

	stop := map[string]context.CancelFunc{}
	stopped := map[string]chan struct{}{}
	for _, name := range []string{"A", "B"} {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		stop[name], stopped[name] = cancel, done // maps are written here only, never in a goroutine

		e := leader.New(pool.Config().ConnConfig, key)
		e.RetryEvery = 50 * time.Millisecond
		e.CheckEvery = 50 * time.Millisecond
		go func() {
			defer close(done)
			e.Run(ctx, func(ctx context.Context) {
				mu.Lock()
				leading = append(leading, name)
				history = append(history, name)
				maxAtOnce = max(maxAtOnce, len(leading))
				mu.Unlock()

				<-ctx.Done()

				mu.Lock()
				leading = leading[:0]
				mu.Unlock()
			})
		}()
	}
	t.Cleanup(func() {
		for name, cancel := range stop {
			cancel()
			<-stopped[name]
		}
	})

	snapshot := func() (int, []string) {
		mu.Lock()
		defer mu.Unlock()
		return maxAtOnce, append([]string(nil), history...)
	}
	waitFor(t, "a first leader", func() bool { _, h := snapshot(); return len(h) == 1 })
	time.Sleep(300 * time.Millisecond) // several retry rounds: the follower must keep failing
	if _, h := snapshot(); len(h) != 1 {
		t.Fatalf("while one process leads, no other may: %v", h)
	}

	// The leader's process shuts down: its session ends, Postgres frees the lock.
	_, h := snapshot()
	first := h[0]
	stop[first]()
	<-stopped[first]

	waitFor(t, "the follower to take over", func() bool { _, h := snapshot(); return len(h) == 2 })
	if m, h := snapshot(); m != 1 || h[1] == first {
		t.Fatalf("want one leader at a time and a takeover; max at once %d, history %v", m, h)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
