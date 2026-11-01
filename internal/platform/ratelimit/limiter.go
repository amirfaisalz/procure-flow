// Package ratelimit implements per-caller token-bucket limiters. Limiting can
// run in-process (Memory) or in Redis so limits hold across API replicas.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Result is the outcome of one Allow call.
type Result struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// Limiter admits or rejects one request for a caller key.
type Limiter interface {
	Allow(ctx context.Context, key string) (Result, error)
	Burst() int
}

// Memory is a thread-safe, in-process token-bucket limiter keyed by caller.
//
// Each key starts with Burst tokens and refills at Rate tokens per second.
// Idle keys are pruned so the map cannot grow without bound.
type Memory struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	now     func() time.Time
	buckets map[string]*bucket
	calls   int
}

type bucket struct {
	tokens float64
	last   time.Time
	seen   time.Time
}

const (
	// sweepInterval is how many Allow calls happen between pruning passes.
	sweepInterval = 1024
	// idleTTL is how long an unused key is kept before being pruned.
	idleTTL = 10 * time.Minute
)

// New creates a limiter that admits Rate requests per second with a Burst
// allowance. A non-positive rate or burst disables limiting.
func New(rate float64, burst int) *Memory {
	return &Memory{
		rate:    rate,
		burst:   float64(burst),
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
}

// Burst returns the configured burst size.
func (l *Memory) Burst() int {
	if l == nil {
		return 0
	}
	return int(l.burst)
}

// Allow consumes one token for key when available.
func (l *Memory) Allow(_ context.Context, key string) (Result, error) {
	if l == nil || l.rate <= 0 || l.burst <= 0 {
		return Result{Allowed: true}, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.calls++
	if l.calls%sweepInterval == 0 {
		l.sweep(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now, seen: now}
		l.buckets[key] = b
	}

	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	b.seen = now

	if b.tokens >= 1 {
		b.tokens--
		return Result{Allowed: true, Remaining: int(b.tokens)}, nil
	}

	deficit := 1 - b.tokens
	retryAfter := time.Duration(deficit / l.rate * float64(time.Second))
	if retryAfter < time.Millisecond {
		retryAfter = time.Millisecond
	}
	return Result{Allowed: false, Remaining: 0, RetryAfter: retryAfter}, nil
}

func (l *Memory) sweep(now time.Time) {
	for key, b := range l.buckets {
		if now.Sub(b.seen) > idleTTL {
			delete(l.buckets, key)
		}
	}
}

// Size reports the number of tracked keys. It is intended for tests.
func (l *Memory) Size() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
