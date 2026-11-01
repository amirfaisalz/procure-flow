package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newClock() *clock {
	return &clock{now: time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC)}
}

func newLimiter(rate float64, burst int, c *clock) *Limiter {
	limiter := New(rate, burst)
	limiter.now = c.Now
	return limiter
}

func TestAllowConsumesBurstThenDenies(t *testing.T) {
	c := newClock()
	limiter := newLimiter(1, 3, c)

	for i := 0; i < 3; i++ {
		result := limiter.Allow("user_amir")
		if !result.Allowed {
			t.Fatalf("request %d denied, want allowed", i)
		}
		if want := 2 - i; result.Remaining != want {
			t.Errorf("request %d remaining = %d, want %d", i, result.Remaining, want)
		}
	}

	denied := limiter.Allow("user_amir")
	if denied.Allowed {
		t.Fatal("fourth request allowed, want denied")
	}
	if denied.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want a positive duration", denied.RetryAfter)
	}
}

func TestAllowRefillsOverTime(t *testing.T) {
	c := newClock()
	limiter := newLimiter(2, 1, c)

	if !limiter.Allow("key").Allowed {
		t.Fatal("first request denied")
	}
	if limiter.Allow("key").Allowed {
		t.Fatal("second request allowed before refill")
	}

	c.Advance(500 * time.Millisecond)
	if !limiter.Allow("key").Allowed {
		t.Fatal("request denied after a full token refilled")
	}
}

func TestAllowIsKeyedPerCaller(t *testing.T) {
	c := newClock()
	limiter := newLimiter(1, 1, c)

	if !limiter.Allow("user_amir").Allowed {
		t.Fatal("amir denied")
	}
	if !limiter.Allow("user_budi").Allowed {
		t.Fatal("budi denied; keys must be independent")
	}
	if limiter.Allow("user_amir").Allowed {
		t.Fatal("amir allowed after exhausting the burst")
	}
}

func TestLimiterPrunesIdleKeys(t *testing.T) {
	c := newClock()
	limiter := newLimiter(1, 1, c)

	for i := 0; i < 10; i++ {
		limiter.Allow(string(rune('a' + i)))
	}
	if got := limiter.Size(); got != 10 {
		t.Fatalf("Size() = %d, want 10", got)
	}

	c.Advance(11 * time.Minute)
	for i := 0; i < 1024; i++ {
		limiter.Allow("fresh")
	}
	if got := limiter.Size(); got != 1 {
		t.Errorf("Size() after sweep = %d, want 1", got)
	}
}

func TestConcurrentAllowRespectsBurst(t *testing.T) {
	c := newClock()
	limiter := newLimiter(1, 10, c)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow("shared").Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != 10 {
		t.Errorf("allowed = %d, want exactly the burst size 10", got)
	}
}

func TestDisabledLimiterAllowsEverything(t *testing.T) {
	for _, limiter := range []*Limiter{nil, New(0, 0)} {
		for i := 0; i < 10; i++ {
			if !limiter.Allow("key").Allowed {
				t.Fatal("disabled limiter denied a request")
			}
		}
	}
}
