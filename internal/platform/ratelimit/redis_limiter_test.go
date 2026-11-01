package ratelimit

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func testRedis(t *testing.T) *redis.Client {
	t.Helper()

	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		t.Skip("TEST_REDIS_URL is not set; skipping Redis integration test")
	}

	opts, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatalf("parse TEST_REDIS_URL: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	return client
}

func redisKey(t *testing.T) string {
	t.Helper()
	return t.Name() + "-" + time.Now().Format("150405.000000000")
}

func TestRedisLimiterBurstThenRefill(t *testing.T) {
	client := testRedis(t)
	limiter := NewRedis(client, 50, 2)
	key := redisKey(t)

	for i := 0; i < 2; i++ {
		result, err := limiter.Allow(context.Background(), key)
		if err != nil {
			t.Fatalf("Allow() error = %v", err)
		}
		if !result.Allowed {
			t.Fatalf("request %d denied, want allowed", i)
		}
	}

	denied, err := limiter.Allow(context.Background(), key)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if denied.Allowed {
		t.Fatal("third request allowed, want denied")
	}
	if denied.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want positive", denied.RetryAfter)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		result, err := limiter.Allow(context.Background(), key)
		if err != nil {
			t.Fatalf("Allow() error = %v", err)
		}
		if result.Allowed {
			return
		}
	}
	t.Fatal("bucket never refilled")
}

func TestRedisLimiterSharesStateAcrossInstances(t *testing.T) {
	client := testRedis(t)
	first := NewRedis(client, 1, 2)
	second := NewRedis(client, 1, 2)
	key := redisKey(t)

	for i := 0; i < 2; i++ {
		if result, err := first.Allow(context.Background(), key); err != nil || !result.Allowed {
			t.Fatalf("first limiter request %d = %+v, %v", i, result, err)
		}
	}

	result, err := second.Allow(context.Background(), key)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if result.Allowed {
		t.Fatal("second limiter allowed a request that the shared bucket should deny")
	}
}

func TestRedisLimiterConcurrentAllowRespectsBurst(t *testing.T) {
	client := testRedis(t)
	limiter := NewRedis(client, 1, 5)
	key := redisKey(t)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := limiter.Allow(context.Background(), key)
			if err != nil {
				t.Errorf("Allow() error = %v", err)
				return
			}
			if result.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != 5 {
		t.Errorf("allowed = %d, want exactly the burst size 5", got)
	}
}

func TestRedisLimiterIsKeyedPerCaller(t *testing.T) {
	client := testRedis(t)
	limiter := NewRedis(client, 1, 1)

	base := redisKey(t)
	amir := base + ":amir"
	budi := base + ":budi"

	if result, err := limiter.Allow(context.Background(), amir); err != nil || !result.Allowed {
		t.Fatalf("amir request = %+v, %v", result, err)
	}
	if result, err := limiter.Allow(context.Background(), amir); err != nil || result.Allowed {
		t.Fatalf("second amir request = %+v, %v; want denied", result, err)
	}
	if result, err := limiter.Allow(context.Background(), budi); err != nil || !result.Allowed {
		t.Fatalf("budi request = %+v, %v; keys must be independent", result, err)
	}
}

// fakeLimiter is a scripted Limiter used to test the fallback wrapper.
type fakeLimiter struct {
	result Result
	err    error
	calls  atomic.Int64
}

func (f *fakeLimiter) Allow(context.Context, string) (Result, error) {
	f.calls.Add(1)
	return f.result, f.err
}

func (f *fakeLimiter) Burst() int { return 7 }

func TestFallbackUsesPrimaryWhenHealthy(t *testing.T) {
	primary := &fakeLimiter{result: Result{Allowed: true, Remaining: 3}}
	secondary := &fakeLimiter{result: Result{Allowed: false}}

	limiter := NewFallback(primary, secondary, slog.New(slog.DiscardHandler))
	result, err := limiter.Allow(context.Background(), "key")
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !result.Allowed || result.Remaining != 3 {
		t.Errorf("result = %+v, want the primary's result", result)
	}
	if secondary.calls.Load() != 0 {
		t.Error("secondary limiter was called while the primary was healthy")
	}
	if got := limiter.Burst(); got != 7 {
		t.Errorf("Burst() = %d, want 7", got)
	}
}

func TestFallbackDegradesToSecondaryOnError(t *testing.T) {
	primary := &fakeLimiter{err: context.DeadlineExceeded}
	secondary := &fakeLimiter{result: Result{Allowed: false, RetryAfter: time.Second}}

	limiter := NewFallback(primary, secondary, slog.New(slog.DiscardHandler))
	result, err := limiter.Allow(context.Background(), "key")
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if result.Allowed {
		t.Error("result allowed, want the secondary limiter's decision")
	}
	if secondary.calls.Load() != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.calls.Load())
	}
}

func TestFallbackAllowsWhenBothBackendsFail(t *testing.T) {
	primary := &fakeLimiter{err: context.DeadlineExceeded}
	secondary := &fakeLimiter{err: context.DeadlineExceeded}

	limiter := NewFallback(primary, secondary, slog.New(slog.DiscardHandler))
	result, err := limiter.Allow(context.Background(), "key")
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !result.Allowed {
		t.Error("result denied, want availability-first allow")
	}
}

func TestFallbackWithoutPrimaryUsesSecondary(t *testing.T) {
	secondary := &fakeLimiter{result: Result{Allowed: true, Remaining: 1}}

	limiter := NewFallback(nil, secondary, nil)
	result, err := limiter.Allow(context.Background(), "key")
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !result.Allowed || result.Remaining != 1 {
		t.Errorf("result = %+v, want the secondary's result", result)
	}
}
