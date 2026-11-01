package repositorytest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
)

var idempotencyBaseTime = time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

// NewIdempotencyRepository returns an empty idempotency repository. It must
// isolate state between calls so each subtest starts from a clean slate.
type NewIdempotencyRepository func(t *testing.T) idempotency.Repository

func idempotencyRecord(key string) idempotency.Record {
	return idempotency.Record{
		ActorID:     "user_amir",
		Key:         key,
		RequestHash: "hash-" + key,
		CreatedAt:   idempotencyBaseTime,
		ExpiresAt:   idempotencyBaseTime.Add(24 * time.Hour),
	}
}

// RunIdempotency executes the idempotency repository contract.
func RunIdempotency(t *testing.T, newRepo NewIdempotencyRepository) {
	t.Helper()

	t.Run("ClaimIsExclusive", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		record := idempotencyRecord("key-1")
		existing, claimed, err := repo.Claim(ctx, record)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if !claimed {
			t.Fatal("first Claim() did not win the claim")
		}
		if existing.Status != 0 {
			t.Errorf("claimed record status = %d, want 0", existing.Status)
		}

		existing, claimed, err = repo.Claim(ctx, record)
		if err != nil {
			t.Fatalf("second Claim() error = %v", err)
		}
		if claimed {
			t.Fatal("second Claim() won a key that was already claimed")
		}
		if !existing.InProgress() {
			t.Errorf("existing = %+v, want an in-progress record", existing)
		}
	})

	t.Run("CompleteIsReplayed", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		record := idempotencyRecord("key-2")
		if _, claimed, err := repo.Claim(ctx, record); err != nil || !claimed {
			t.Fatalf("Claim() = claimed %v, error %v", claimed, err)
		}

		body := []byte(`{"data":{"id":"prc_1"}}`)
		if err := repo.Complete(ctx, record.ActorID, record.Key, 201, "application/json", "/api/v1/procurements/prc_1", body); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}

		existing, claimed, err := repo.Claim(ctx, record)
		if err != nil {
			t.Fatalf("Claim() after complete error = %v", err)
		}
		if claimed {
			t.Fatal("Claim() reclaimed a completed key")
		}
		if existing.Status != 201 {
			t.Errorf("Status = %d, want 201", existing.Status)
		}
		if existing.ContentType != "application/json" {
			t.Errorf("ContentType = %q", existing.ContentType)
		}
		if existing.Location != "/api/v1/procurements/prc_1" {
			t.Errorf("Location = %q", existing.Location)
		}
		if string(existing.Body) != string(body) {
			t.Errorf("Body = %q, want %q", existing.Body, body)
		}
	})

	t.Run("ReleaseAllowsReclaim", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		record := idempotencyRecord("key-3")
		if _, claimed, err := repo.Claim(ctx, record); err != nil || !claimed {
			t.Fatalf("Claim() = claimed %v, error %v", claimed, err)
		}
		if err := repo.Release(ctx, record.ActorID, record.Key); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
		if _, claimed, err := repo.Claim(ctx, record); err != nil || !claimed {
			t.Fatalf("Claim() after release = claimed %v, error %v", claimed, err)
		}
	})

	t.Run("ActorsAreIsolated", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		first := idempotencyRecord("shared-key")
		second := idempotencyRecord("shared-key")
		second.ActorID = "user_budi"
		second.RequestHash = "hash-budi"

		if _, claimed, err := repo.Claim(ctx, first); err != nil || !claimed {
			t.Fatalf("first actor Claim() = claimed %v, error %v", claimed, err)
		}
		if _, claimed, err := repo.Claim(ctx, second); err != nil || !claimed {
			t.Fatalf("second actor Claim() = claimed %v, error %v", claimed, err)
		}
	})

	t.Run("ExpiredClaimIsReclaimed", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		record := idempotencyRecord("key-expired")
		record.ExpiresAt = idempotencyBaseTime.Add(-time.Minute)
		if _, claimed, err := repo.Claim(ctx, record); err != nil || !claimed {
			t.Fatalf("expired Claim() = claimed %v, error %v", claimed, err)
		}

		fresh := idempotencyRecord("key-expired")
		fresh.RequestHash = "hash-fresh"
		fresh.CreatedAt = idempotencyBaseTime.Add(time.Minute)
		fresh.ExpiresAt = idempotencyBaseTime.Add(25 * time.Hour)
		_, claimed, err := repo.Claim(ctx, fresh)
		if err != nil {
			t.Fatalf("reclaim error = %v", err)
		}
		if !claimed {
			t.Fatal("Claim() did not reclaim an expired record")
		}
	})

	t.Run("ConcurrentClaimsOnlyOneWins", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		const workers = 20
		var wg sync.WaitGroup
		claimedCount := make(chan int, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				record := idempotencyRecord("key-concurrent")
				record.RequestHash = fmt.Sprintf("hash-%d", i)
				_, claimed, err := repo.Claim(ctx, record)
				if err != nil {
					claimedCount <- -1
					return
				}
				if claimed {
					claimedCount <- 1
					return
				}
				claimedCount <- 0
			}(i)
		}
		wg.Wait()
		close(claimedCount)

		winners := 0
		for result := range claimedCount {
			if result < 0 {
				t.Fatal("Claim() returned an error")
			}
			winners += result
		}
		if winners != 1 {
			t.Errorf("claim winners = %d, want exactly 1", winners)
		}
	})
}
