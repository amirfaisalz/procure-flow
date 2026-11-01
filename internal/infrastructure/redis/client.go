// Package redis adapts a Redis client to the platform ports: the shared rate
// limiter and the read-through aggregate cache.
package redis

import (
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// NewClient parses a redis:// or rediss:// URL. It does not dial: the client
// connects lazily and reconnects on demand, which keeps the API available when
// Redis restarts. Timeouts stay short because Redis is an optimization, not a
// system of record.
func NewClient(url string, dialTimeout, commandTimeout time.Duration) (*goredis.Client, error) {
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: parse URL: %w", err)
	}
	opts.DialTimeout = dialTimeout
	opts.ReadTimeout = commandTimeout
	opts.WriteTimeout = commandTimeout
	opts.PoolTimeout = dialTimeout
	opts.MaxRetries = 1

	return goredis.NewClient(opts), nil
}
