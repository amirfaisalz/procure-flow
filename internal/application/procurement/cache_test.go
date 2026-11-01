package procurement_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

// fakeCache stores aggregates in memory and records calls. It mirrors the
// generation fence of the Redis adapter.
type fakeCache struct {
	items       map[string]domainproc.Procurement
	generations map[string]string
	nextGen     int
	getErr      error
	setErr      error
	deleteErr   error
	getCalls    int
	setCalls    int
	deleteCalls int
	lastTTL     time.Duration
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		items:       make(map[string]domainproc.Procurement),
		generations: make(map[string]string),
	}
}

func (f *fakeCache) Get(_ context.Context, key string, dst any) (bool, string, error) {
	f.getCalls++
	if f.getErr != nil {
		return false, "", f.getErr
	}
	item, ok := f.items[key]
	if !ok {
		return false, f.generations[key], nil
	}
	ptr, ok := dst.(*domainproc.Procurement)
	if !ok {
		return false, f.generations[key], errors.New("fakeCache: unexpected destination type")
	}
	*ptr = item
	return true, f.generations[key], nil
}

func (f *fakeCache) SetIfGeneration(_ context.Context, key string, value any, ttl time.Duration, generation string) (bool, error) {
	f.setCalls++
	if f.setErr != nil {
		return false, f.setErr
	}
	if f.generations[key] != generation {
		return false, nil
	}
	item, ok := value.(domainproc.Procurement)
	if !ok {
		return false, errors.New("fakeCache: unexpected value type")
	}
	f.items[key] = item
	f.lastTTL = ttl
	return true, nil
}

func (f *fakeCache) Delete(_ context.Context, key string) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.items, key)
	f.nextGen++
	f.generations[key] = fmt.Sprintf("gen-%d", f.nextGen)
	return nil
}

func newCachedService(repo *fakeRepo, cache *fakeCache) *app.Service {
	return app.NewService(repo, fakeIDs{id: "prc_1", number: "PR-202610-0001"}, func() time.Time {
		return fixedNow
	}, app.WithCache(cache, 30*time.Second))
}

func TestServiceGetCachesAggregate(t *testing.T) {
	repo := &fakeRepo{item: sampleProcurement(), has: true}
	cache := newFakeCache()
	svc := newCachedService(repo, cache)

	first, err := svc.Get(adminCtx(), "prc_1")
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	if first.ID != "prc_1" {
		t.Fatalf("first Get() ID = %q, want prc_1", first.ID)
	}
	if cache.setCalls != 1 {
		t.Errorf("cache set calls = %d, want 1", cache.setCalls)
	}
	if cache.lastTTL != 30*time.Second {
		t.Errorf("cache TTL = %v, want 30s", cache.lastTTL)
	}

	if _, err := svc.Get(adminCtx(), "prc_1"); err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if repo.getCalls != 1 {
		t.Errorf("repository GetByID calls = %d, want 1 (second read should hit cache)", repo.getCalls)
	}
}

func TestServiceGetDoesNotCacheAfterConcurrentInvalidation(t *testing.T) {
	repo := &fakeRepo{item: sampleProcurement(), has: true}
	cache := newFakeCache()
	key := procurementCacheKeyForTest("prc_1")

	// Simulate a mutation committing (and invalidating) between the cache miss
	// and the database read.
	repo.afterGet = func() {
		_ = cache.Delete(context.Background(), key)
	}

	svc := newCachedService(repo, cache)
	if _, err := svc.Get(adminCtx(), "prc_1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if cache.setCalls != 1 {
		t.Errorf("cache set calls = %d, want 1 attempt", cache.setCalls)
	}
	if len(cache.items) != 0 {
		t.Error("a stale aggregate was cached after the concurrent invalidation")
	}
}

func TestServiceGetServesCachedValue(t *testing.T) {
	repo := &fakeRepo{has: false}
	cache := newFakeCache()
	cached := sampleProcurement()
	cached.Title = "Cached title"
	cache.items[procurementCacheKeyForTest("prc_1")] = cached

	svc := newCachedService(repo, cache)
	got, err := svc.Get(adminCtx(), "prc_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "Cached title" {
		t.Errorf("Title = %q, want the cached value", got.Title)
	}
	if repo.getCalls != 0 {
		t.Error("repository was queried on a cache hit")
	}
}

func TestServiceGetFallsBackWhenCacheFails(t *testing.T) {
	repo := &fakeRepo{item: sampleProcurement(), has: true}
	cache := newFakeCache()
	cache.getErr = errors.New("redis down")
	cache.setErr = errors.New("redis down")

	svc := newCachedService(repo, cache)
	got, err := svc.Get(adminCtx(), "prc_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != "prc_1" {
		t.Errorf("ID = %q, want prc_1", got.ID)
	}
}

func TestServiceMutationInvalidatesCache(t *testing.T) {
	repo := &fakeRepo{item: sampleProcurement(), has: true}
	cache := newFakeCache()
	svc := newCachedService(repo, cache)

	if _, err := svc.Submit(adminCtx(), "prc_1"); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if cache.deleteCalls != 1 {
		t.Errorf("cache delete calls = %d, want 1", cache.deleteCalls)
	}
}

func TestServiceMutationKeepsCacheWhenItCannotInvalidate(t *testing.T) {
	repo := &fakeRepo{item: sampleProcurement(), has: true}
	cache := newFakeCache()
	cache.deleteErr = errors.New("redis down")
	svc := newCachedService(repo, cache)

	if _, err := svc.Submit(adminCtx(), "prc_1"); err != nil {
		t.Fatalf("Submit() error = %v; cache failures must not fail writes", err)
	}
}

func TestServiceGetDoesNotCacheNotFound(t *testing.T) {
	repo := &fakeRepo{has: false}
	cache := newFakeCache()
	svc := newCachedService(repo, cache)

	if _, err := svc.Get(adminCtx(), "prc_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if cache.setCalls != 0 {
		t.Errorf("cache set calls = %d, want 0 for a missing aggregate", cache.setCalls)
	}
}

func sampleProcurement() domainproc.Procurement {
	return domainproc.Procurement{
		ID:          "prc_1",
		Number:      "PR-202610-0001",
		Title:       "Laptop procurement for engineering",
		Description: "20 units",
		Department:  "Engineering",
		Requester:   "Amir",
		RequesterID: "user_admin",
		Amount:      250_000_000,
		Currency:    "IDR",
		Status:      domainproc.StatusDraft,
		CreatedAt:   fixedNow,
		UpdatedAt:   fixedNow,
		Version:     1,
	}
}

func procurementCacheKeyForTest(id string) string {
	return "procurement:v1:" + id
}
