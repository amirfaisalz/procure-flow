package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Cache is a JSON read-through cache backed by Redis.
type Cache struct {
	client *goredis.Client
	prefix string
}

// NewCache creates a cache that namespaces every key with prefix.
func NewCache(client *goredis.Client, prefix string) *Cache {
	if prefix == "" {
		prefix = "cache:"
	}
	return &Cache{client: client, prefix: prefix}
}

// Get decodes the cached JSON value into dst. A missing key is (false, nil);
// a corrupt value is deleted and reported as a miss so tenants self-heal.
func (c *Cache) Get(ctx context.Context, key string, dst any) (bool, error) {
	raw, err := c.client.Get(ctx, c.prefix+key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache: get %q: %w", key, err)
	}

	if err := json.Unmarshal(raw, dst); err != nil {
		if delErr := c.client.Del(ctx, c.prefix+key).Err(); delErr != nil {
			return false, fmt.Errorf("cache: evict corrupt %q: %w", key, delErr)
		}
		return false, nil
	}
	return true, nil
}

// Set stores value as JSON with the given TTL.
func (c *Cache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: encode %q: %w", key, err)
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	if err := c.client.Set(ctx, c.prefix+key, raw, ttl).Err(); err != nil {
		return fmt.Errorf("cache: set %q: %w", key, err)
	}
	return nil
}

// Delete removes key. Deleting a missing key is not an error.
func (c *Cache) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, c.prefix+key).Err(); err != nil {
		return fmt.Errorf("cache: delete %q: %w", key, err)
	}
	return nil
}
