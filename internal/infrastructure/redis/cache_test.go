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

func TestCacheSetGetDelete(t *testing.T) {
	cache, client, prefix := testCache(t)
	ctx := context.Background()

	want := cachedProcurement{ID: "prc_1", Status: "draft"}
	if err := cache.Set(ctx, "procurement:prc_1", want, time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	var got cachedProcurement
	ok, err := cache.Get(ctx, "procurement:prc_1", &got)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok || got != want {
		t.Errorf("Get() = %+v, %v; want %+v, true", got, ok, want)
	}

	ttl := client.TTL(ctx, prefix+"procurement:prc_1").Val()
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("TTL = %v, want a positive duration up to 1m", ttl)
	}

	if err := cache.Delete(ctx, "procurement:prc_1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	var after cachedProcurement
	ok, err = cache.Get(ctx, "procurement:prc_1", &after)
	if err != nil {
		t.Fatalf("Get() after delete error = %v", err)
	}
	if ok {
		t.Error("Get() after delete reported a hit")
	}
}

func TestCacheMiss(t *testing.T) {
	cache, _, _ := testCache(t)

	var dst cachedProcurement
	ok, err := cache.Get(context.Background(), "absent", &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() reported a hit for a missing key")
	}
}

func TestCacheCorruptValueIsEvicted(t *testing.T) {
	cache, client, prefix := testCache(t)
	ctx := context.Background()

	if err := client.Set(ctx, prefix+"corrupt", "{not json", time.Minute).Err(); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}

	var dst cachedProcurement
	ok, err := cache.Get(ctx, "corrupt", &dst)
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

	if err := cache.Set(ctx, "short", cachedProcurement{ID: "prc_1"}, 50*time.Millisecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	var dst cachedProcurement
	ok, err := cache.Get(ctx, "short", &dst)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok {
		t.Error("Get() reported a hit after the TTL expired")
	}
}
