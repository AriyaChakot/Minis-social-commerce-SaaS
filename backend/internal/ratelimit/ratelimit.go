// Package ratelimit limits how often one client (an IP address, an email...) may do something,
// using a token bucket per key.
//
// Token bucket: every key has a bucket that holds up to Burst tokens and starts full. Each
// request takes one token; a request that finds the bucket empty is refused. Tokens come back at
// a steady rate (one every Every), so a client may make Burst quick requests, then one per Every.
//
// The buckets live in this process's memory: with several API replicas, each one counts on its
// own (N replicas allow N times the limit). That is acceptable as a first line of defense; a
// shared store (Redis, or a Postgres table) is the next step if exact global limits matter.
package ratelimit

import (
	"math"
	"net/netip"
	"sync"
	"time"
)

// Limiter is safe for concurrent use.
type Limiter struct {
	burst float64
	every time.Duration
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time // when tokens was last brought up to date
}

// New returns a limiter allowing burst requests at once per key, then one per every.
func New(burst int, every time.Duration) *Limiter {
	return newWithClock(burst, every, time.Now)
}

func newWithClock(burst int, every time.Duration, now func() time.Time) *Limiter {
	return &Limiter{
		burst:     float64(burst),
		every:     every,
		now:       now,
		buckets:   make(map[string]*bucket),
		lastSweep: now(),
	}
}

// Allow takes a token from key's bucket. When the bucket is empty it returns false and how long
// until the next token arrives (for the Retry-After header).
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	// Instead of a timer that adds tokens, compute how many arrived since the last visit.
	elapsed := now.Sub(b.last)
	b.tokens = math.Min(l.burst, b.tokens+elapsed.Seconds()/l.every.Seconds())
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	missing := 1 - b.tokens
	// Rounded to the millisecond: float arithmetic would otherwise give 5.999999999s for 6s.
	wait := time.Duration(math.Round(missing*float64(l.every)/float64(time.Millisecond))) * time.Millisecond
	return false, wait
}

// IPKey turns a client IP into a rate-limit key. IPv4 addresses are used as they are. For IPv6,
// one home or phone usually gets a whole /64 network (2^64 addresses), so a client could
// otherwise switch to a fresh address for every request; the key is the /64 prefix instead.
func IPKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap() // "::ffff:192.0.2.1" is just an IPv4 address
	if addr.Is6() {
		prefix, _ := addr.Prefix(64)
		return prefix.String()
	}
	return addr.String()
}

// sweep forgets buckets that have refilled completely: a full bucket is the same as no bucket,
// so dropping it changes nothing for that client. Without this, every IP address ever seen
// would stay in memory forever. It runs at most once per refill period, under l.mu.
func (l *Limiter) sweep(now time.Time) {
	full := time.Duration(l.burst * float64(l.every)) // time for an empty bucket to fill up
	if now.Sub(l.lastSweep) < full {
		return
	}
	for key, b := range l.buckets {
		if now.Sub(b.last) >= full {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}

// Len reports how many keys are being tracked (for tests and metrics).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
