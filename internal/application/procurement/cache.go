package procurement

import (
	"context"
	"strings"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
)

// Cache is an optional read-through cache for aggregates. Get must report a
// missing key as (false, nil); errors are treated as misses by the service.
type Cache interface {
	Get(ctx context.Context, key string, dst any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
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
