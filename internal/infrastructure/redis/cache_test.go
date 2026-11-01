package redis

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func testClient(t *testing.T) *goredis.Client {
	t.Helper()

	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		t.Skip("TEST_REDIS_URL is not set; skipping Redis integration test")
	}
	opts, err := goredis.ParseURL(raw)
	if err != nil {
		t.Fatalf("parse TEST_REDIS_URL: %v", err)
	}
	client := goredis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	return client
}

type cachedProcurement struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func testCache(t *testing.T) (*Cache, *goredis.Client, string) {
	t.Helper()
	client := testClient(t)
	prefix := "test:" + t.Name() + ":"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		keys, _ := client.Keys(ctx, prefix+"*").Result()
		if len(keys) > 0 {
			_ = client.Del(ctx, keys...).Err()
		}
	})
	return NewCache(client, prefix), client, prefix
}

func TestCacheSetIfGenerationAndGet(t *testing.T) {
	cache, client, prefix := testCache(t)
	ctx := context.Background()
	const key = "procurement:prc_1"

	var miss cachedProcurement
	found, generation, err := cache.Get(ctx, key, &miss)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found {
		t.Fatal("Get() reported a hit for a missing key")
	}

	want := cachedProcurement{ID: "prc_1", Status: "draft"}
	written, err := cache.SetIfGeneration(ctx, key, want, time.Minute, generation)
	if err != nil {
		t.Fatalf("SetIfGeneration() error = %v", err)
	}
	if !written {
		t.Fatal("SetIfGeneration() did not write with the current generation")
	}

	var got cachedProcurement
	found, storedGeneration, err := cache.Get(ctx, key, &got)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found || got != want {
		t.Errorf("Get() = %+v, %v; want %+v, true", got, found, want)
	}
	if storedGeneration != generation {
		t.Errorf("generation = %q, want %q", storedGeneration, generation)
	}

	ttl := client.TTL(ctx, prefix+key).Val()
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("TTL = %v, want a positive duration up to 1m", ttl)
	}
}

func TestCacheRejectsStaleGeneration(t *testing.T) {
	cache, _, _ := testCache(t)
	ctx := context.Background()
	const key = "procurement:prc_1"

	var dst cachedProcurement
	_, staleGeneration, err := cache.Get(ctx, key, &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// A mutation invalidates the entry and advances the generation.
	if err := cache.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	written, err := cache.SetIfGeneration(ctx, key, cachedProcurement{ID: "prc_1"}, time.Minute, staleGeneration)
	if err != nil {
		t.Fatalf("SetIfGeneration() error = %v", err)
	}
	if written {
		t.Fatal("SetIfGeneration() wrote with a stale generation")
	}

	ok, _, err := cache.Get(ctx, key, &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() returned the value written with a stale generation")
	}
}

func TestCacheDeleteAdvancesGeneration(t *testing.T) {
	cache, _, _ := testCache(t)
	ctx := context.Background()
	const key = "procurement:prc_1"

	var dst cachedProcurement
	_, before, err := cache.Get(ctx, key, &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if err := cache.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, after, err := cache.Get(ctx, key, &dst)
	if err != nil {
		t.Fatalf("Get() after delete error = %v", err)
	}
	if after == before {
		t.Errorf("generation = %q after Delete, want a new value", after)
	}
}

func TestCacheCorruptValueIsEvicted(t *testing.T) {
	cache, client, prefix := testCache(t)
	ctx := context.Background()

	if err := client.Set(ctx, prefix+"corrupt", "{not json", time.Minute).Err(); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}

	var dst cachedProcurement
	ok, _, err := cache.Get(ctx, "corrupt", &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() reported a hit for corrupt JSON")
	}
	if n := client.Exists(ctx, prefix+"corrupt").Val(); n != 0 {
		t.Error("corrupt cache entry was not deleted")
	}
}

func TestCacheTTLExpires(t *testing.T) {
	cache, _, _ := testCache(t)
	ctx := context.Background()

	var dst cachedProcurement
	_, generation, err := cache.Get(ctx, "short", &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := cache.SetIfGeneration(ctx, "short", cachedProcurement{ID: "prc_1"}, 50*time.Millisecond, generation); err != nil {
		t.Fatalf("SetIfGeneration() error = %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	ok, _, err := cache.Get(ctx, "short", &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() reported a hit after the TTL expired")
	}
}
