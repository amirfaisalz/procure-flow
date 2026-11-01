package procurement

import (
	"context"
	"strings"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
)

// Cache is an optional read-through cache for aggregates. Mutations advance a
// per-key generation, so a read that started before the mutation cannot
// repopulate the cache with stale data afterwards.
type Cache interface {
	// Get returns the cached value and the generation observed for key. A miss
	// returns (false, generation, nil); the caller passes that generation to
	// SetIfGeneration after reading the system of record.
	Get(ctx context.Context, key string, dst any) (found bool, generation string, err error)
	// SetIfGeneration stores value only when the key's generation still equals
	// generation, and reports whether it wrote.
	SetIfGeneration(ctx context.Context, key string, value any, ttl time.Duration, generation string) (bool, error)
	// Delete invalidates key and advances its generation.
	Delete(ctx context.Context, key string) error
}

// defaultCacheTTL bounds how long a stale aggregate can be served if an
// invalidation races with a concurrent read.
const defaultCacheTTL = 30 * time.Second

func procurementCacheKey(id string) string {
	return "procurement:v1:" + id
}

func normalizeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", &domain.ValidationError{Fields: map[string]string{"id": "must not be empty"}}
	}
	return id, nil
}
