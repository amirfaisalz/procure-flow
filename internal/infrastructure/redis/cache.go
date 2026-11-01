package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// generationTTL keeps a generation marker alive far longer than any in-flight
// read (request deadlines are measured in seconds), so a read that observed the
// previous generation cannot repopulate stale data. It is independent of the
// data TTL and bounded to keep marker keys from accumulating for a full day.
const generationTTL = time.Hour

const generationSuffix = ":gen"

// Cache is a JSON read-through cache backed by Redis. Every entry carries a
// generation marker: Delete advances it, and writes only succeed while it is
// unchanged, so a read that raced with a mutation cannot cache stale data.
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

// Get decodes the cached JSON value into dst and returns the generation
// observed for the key. A missing key is (false, generation, nil); a corrupt
// value is deleted and reported as a miss so tenants self-heal.
func (c *Cache) Get(ctx context.Context, key string, dst any) (bool, string, error) {
	dataKey, genKey := c.keys(key)

	values, err := c.client.MGet(ctx, dataKey, genKey).Result()
	if err != nil {
		return false, "", fmt.Errorf("cache: get %q: %w", key, err)
	}

	generation := ""
	if len(values) > 1 && values[1] != nil {
		if value, ok := values[1].(string); ok {
			generation = value
		}
	}
	if len(values) == 0 || values[0] == nil {
		return false, generation, nil
	}

	raw, ok := values[0].(string)
	if !ok {
		return false, generation, fmt.Errorf("cache: get %q: unexpected value type %T", key, values[0])
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		if delErr := c.client.Del(ctx, dataKey).Err(); delErr != nil {
			return false, generation, fmt.Errorf("cache: evict corrupt %q: %w", key, delErr)
		}
		return false, generation, nil
	}
	return true, generation, nil
}

// SetIfGeneration stores value as JSON when the key's generation still matches
// generation. It reports whether the value was written.
func (c *Cache) SetIfGeneration(ctx context.Context, key string, value any, ttl time.Duration, generation string) (bool, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("cache: encode %q: %w", key, err)
	}
	if ttl <= 0 {
		ttl = time.Minute
	}

	dataKey, genKey := c.keys(key)
	written, err := setIfGenerationScript.Run(ctx, c.client, []string{dataKey, genKey},
		generation, raw, ttl.Milliseconds(), generationTTL.Milliseconds(),
	).Int()
	if err != nil {
		return false, fmt.Errorf("cache: set %q: %w", key, err)
	}
	return written == 1, nil
}

// Delete removes key and advances its generation. Deleting a missing key is not
// an error; the new generation still invalidates any in-flight read.
func (c *Cache) Delete(ctx context.Context, key string) error {
	dataKey, genKey := c.keys(key)
	if err := deleteScript.Run(ctx, c.client, []string{dataKey, genKey},
		newGeneration(), generationTTL.Milliseconds(),
	).Err(); err != nil {
		return fmt.Errorf("cache: delete %q: %w", key, err)
	}
	return nil
}

func (c *Cache) keys(key string) (dataKey, genKey string) {
	dataKey = c.prefix + key
	return dataKey, dataKey + generationSuffix
}

// getSetGeneration compares the stored generation and writes the value only
// when it is unchanged. Setting the marker to the expected value (including an
// empty one) keeps subsequent reads consistent and refreshes its TTL.
var setIfGenerationScript = goredis.NewScript(`
local current = redis.call('GET', KEYS[2])
if not current then current = '' end
if current ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[4])
return 1
`)

// deleteScript removes the value and installs a fresh generation atomically, so
// there is no window in which stale in-flight reads can write.
var deleteScript = goredis.NewScript(`
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[2])
return 1
`)

var generationCounter atomic.Uint64

func newGeneration() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return strconv.FormatUint(generationCounter.Add(1), 16) + "-" + strconv.FormatInt(time.Now().UnixNano(), 16)
}
