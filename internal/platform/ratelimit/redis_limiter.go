package ratelimit

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter keeps one token bucket per caller in Redis so limits are shared
// by every API replica. The bucket is updated by a single Lua script, making
// each Allow call atomic across processes.
type RedisLimiter struct {
	client redis.UniversalClient
	rate   float64
	burst  int
	prefix string
	ttl    time.Duration
	now    func() time.Time
}

// NewRedis creates a Redis-backed limiter. Callers own the client lifecycle.
func NewRedis(client redis.UniversalClient, rate float64, burst int) *RedisLimiter {
	limiter := &RedisLimiter{
		client: client,
		rate:   rate,
		burst:  burst,
		prefix: "ratelimit:",
		now:    time.Now,
	}
	if rate > 0 && burst > 0 {
		// A bucket that has been idle for this long is full again, so the key
		// can expire without changing any observable behaviour.
		refill := time.Duration(math.Ceil(float64(burst)/rate)) * time.Second
		limiter.ttl = 2*refill + time.Second
	}
	return limiter
}

// Burst returns the configured burst size.
func (l *RedisLimiter) Burst() int {
	if l == nil {
		return 0
	}
	return l.burst
}

// tokenBucketScript refills the bucket, consumes one token when possible, and
// returns [allowed, remaining, retry_after_ms]. Time is passed in by the
// caller so replicas with skewed clocks still make monotonic decisions.
var tokenBucketScript = redis.NewScript(`
local values = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(values[1])
local ts = tonumber(values[2])
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

if tokens == nil then
  tokens = burst
  ts = now
end

local elapsed = now - ts
if elapsed < 0 then
  elapsed = 0
end
tokens = math.min(burst, tokens + elapsed * rate)

local allowed = 0
if tokens >= 1 then
  allowed = 1
  tokens = tokens - 1
end

local retry = 0
if allowed == 0 then
  retry = math.ceil((1 - tokens) / rate * 1000)
  if retry < 1 then
    retry = 1
  end
end

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', tostring(now))
redis.call('PEXPIRE', KEYS[1], ttl)
return {allowed, math.floor(tokens), retry}
`)

// Allow consumes one token for key when available.
func (l *RedisLimiter) Allow(ctx context.Context, key string) (Result, error) {
	if l == nil || l.rate <= 0 || l.burst <= 0 {
		return Result{Allowed: true}, nil
	}
	if l.client == nil {
		return Result{}, fmt.Errorf("ratelimit: redis client is nil")
	}

	now := float64(l.now().UnixMilli()) / 1000
	values, err := tokenBucketScript.Run(ctx, l.client, []string{l.prefix + key},
		l.rate, float64(l.burst), now, l.ttl.Milliseconds(),
	).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: redis token bucket: %w", err)
	}
	if len(values) != 3 {
		return Result{}, fmt.Errorf("ratelimit: unexpected script result %v", values)
	}

	if values[0] == 1 {
		return Result{Allowed: true, Remaining: int(values[1])}, nil
	}
	return Result{
		Allowed:    false,
		Remaining:  int(values[1]),
		RetryAfter: time.Duration(values[2]) * time.Millisecond,
	}, nil
}
