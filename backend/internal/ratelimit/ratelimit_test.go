package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClock lets the tests move time forward instantly instead of sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestBurstThenSteadyRate(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	l := newWithClock(3, 10*time.Second, clock.now)

	for i := range 3 {
		if ok, _ := l.Allow("ip-1"); !ok {
			t.Fatalf("request %d of the burst was refused", i+1)
		}
	}
	ok, wait := l.Allow("ip-1")
	if ok || wait != 10*time.Second {
		t.Fatalf("4th request: want refused with 10s to wait, got ok=%v wait=%s", ok, wait)
	}

	clock.advance(4 * time.Second)
	if ok, wait := l.Allow("ip-1"); ok || wait != 6*time.Second {
		t.Fatalf("after 4s: want refused with 6s left, got ok=%v wait=%s", ok, wait)
	}

	clock.advance(6 * time.Second) // one token has arrived
	if ok, _ := l.Allow("ip-1"); !ok {
		t.Fatal("after 10s one more request must be allowed")
	}
	if ok, _ := l.Allow("ip-1"); ok {
		t.Fatal("...but only one")
	}

	clock.advance(time.Hour) // a long pause refills to Burst, never above it
	for i := range 3 {
		if ok, _ := l.Allow("ip-1"); !ok {
			t.Fatalf("after a pause, request %d was refused", i+1)
		}
	}
	if ok, _ := l.Allow("ip-1"); ok {
		t.Fatal("the bucket must not hold more than Burst tokens")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	l := newWithClock(1, time.Minute, clock.now)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a: first request refused")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a: second request allowed")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b must not be affected by a")
	}
}

func TestIdleKeysAreForgotten(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	l := newWithClock(2, time.Second, clock.now)
	for i := range 1000 {
		l.Allow(fmt.Sprintf("ip-%d", i))
	}
	if l.Len() != 1000 {
		t.Fatalf("want 1000 tracked keys, got %d", l.Len())
	}
	clock.advance(2 * time.Second) // every bucket has refilled
	l.Allow("new")
	if l.Len() != 1 {
		t.Fatalf("idle keys must be dropped, %d left", l.Len())
	}
}

// TestConcurrentAllow: 100 goroutines race for a bucket of 10; exactly 10 must win.
// Run with -race to also check the locking.
func TestConcurrentAllow(t *testing.T) {
	l := New(10, time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 100 {
		wg.Go(func() {
			if ok, _ := l.Allow("shared"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 10 {
		t.Fatalf("want exactly 10 allowed, got %d", allowed)
	}
}

func TestIPKey(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.10":              "192.0.2.10",
		"::ffff:192.0.2.10":       "192.0.2.10",
		"2001:db8:1:2:aaaa::1":    "2001:db8:1:2::/64",
		"2001:db8:1:2:bbbb::9999": "2001:db8:1:2::/64", // same household, same key
		"2001:db8:1:3::1":         "2001:db8:1:3::/64",
		"not-an-ip":               "not-an-ip",
	} {
		if got := IPKey(in); got != want {
			t.Errorf("IPKey(%q) = %q, want %q", in, got, want)
		}
	}
}
