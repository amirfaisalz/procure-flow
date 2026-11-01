package testsupport_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
)

var baseTime = time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

func newProcurement(t *testing.T, id, number, title, department string, createdAt time.Time) procurement.Procurement {
	t.Helper()

	p, err := procurement.New(id, number, procurement.CreateParams{
		Title:       title,
		Description: "test description",
		Department:  department,
		Requester:   "Amir",
		Amount:      1_000_000,
		Currency:    "IDR",
	}, createdAt)
	if err != nil {
		t.Fatalf("procurement.New() error = %v", err)
	}
	return p
}

func TestRepositoryCreateAndGet(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.NewProcurementRepository()
	p := newProcurement(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)

	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ID != p.ID || got.Number != p.Number || got.Title != p.Title {
		t.Errorf("GetByID() = %+v, want %+v", got, p)
	}

	if _, err := repo.GetByID(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetByID(missing) error = %v, want ErrNotFound", err)
	}

	if err := repo.Create(ctx, p); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate Create() error = %v, want ErrConflict", err)
	}
}

func TestRepositoryListFilterAndPagination(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.NewProcurementRepository()

	items := []procurement.Procurement{
		newProcurement(t, "prc_1", "PR-202610-0001", "Laptop engineering", "Engineering", baseTime.Add(1*time.Minute)),
		newProcurement(t, "prc_2", "PR-202610-0002", "Office chairs", "Facilities", baseTime.Add(2*time.Minute)),
		newProcurement(t, "prc_3", "PR-202610-0003", "Laptop support", "Support", baseTime.Add(3*time.Minute)),
		newProcurement(t, "prc_4", "PR-202610-0004", "Server rack", "Engineering", baseTime.Add(4*time.Minute)),
		newProcurement(t, "prc_5", "PR-202610-0005", "Marketing campaign", "Marketing", baseTime.Add(5*time.Minute)),
	}
	items[0].Status = procurement.StatusSubmitted
	items[2].Status = procurement.StatusSubmitted
	items[3].Status = procurement.StatusApproved
	items[4].Status = procurement.StatusRejected

	for _, item := range items {
		if err := repo.Create(ctx, item); err != nil {
			t.Fatalf("Create(%s) error = %v", item.ID, err)
		}
	}

	t.Run("all items are newest first", func(t *testing.T) {
		got, err := repo.List(ctx, procurement.ListFilter{Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if got.Total != 5 || len(got.Items) != 5 {
			t.Fatalf("Total/len = %d/%d, want 5/5", got.Total, len(got.Items))
		}
		if got.Items[0].ID != "prc_5" || got.Items[4].ID != "prc_1" {
			t.Errorf("order = %s..%s, want prc_5..prc_1", got.Items[0].ID, got.Items[4].ID)
		}
	})

	t.Run("filters by status", func(t *testing.T) {
		got, err := repo.List(ctx, procurement.ListFilter{Status: procurement.StatusSubmitted, Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if got.Total != 2 {
			t.Fatalf("Total = %d, want 2", got.Total)
		}
		for _, item := range got.Items {
			if item.Status != procurement.StatusSubmitted {
				t.Errorf("item %s status = %q, want submitted", item.ID, item.Status)
			}
		}
	})

	t.Run("searches case-insensitively across fields", func(t *testing.T) {
		got, err := repo.List(ctx, procurement.ListFilter{Query: "LAPTOP", Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if got.Total != 2 {
			t.Fatalf("Total for LAPTOP = %d, want 2", got.Total)
		}

		got, err = repo.List(ctx, procurement.ListFilter{Query: "engineering", Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if got.Total != 2 {
			t.Fatalf("Total for engineering = %d, want 2", got.Total)
		}
	})

	t.Run("paginates", func(t *testing.T) {
		got, err := repo.List(ctx, procurement.ListFilter{Page: 2, PageSize: 2})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if got.Total != 5 {
			t.Errorf("Total = %d, want 5", got.Total)
		}
		if len(got.Items) != 2 {
			t.Fatalf("len(Items) = %d, want 2", len(got.Items))
		}
		if got.Items[0].ID != "prc_3" || got.Items[1].ID != "prc_2" {
			t.Errorf("page 2 order = %s, %s; want prc_3, prc_2", got.Items[0].ID, got.Items[1].ID)
		}
		if got.TotalPages() != 3 {
			t.Errorf("TotalPages() = %d, want 3", got.TotalPages())
		}
	})

	t.Run("empty result is not nil", func(t *testing.T) {
		got, err := repo.List(ctx, procurement.ListFilter{Query: "does-not-exist", Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if got.Items == nil {
			t.Error("Items = nil, want empty slice")
		}
	})
}

func TestRepositoryUpdateOptimisticConcurrency(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.NewProcurementRepository()
	p := newProcurement(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updated := p
	if err := updated.Submit(baseTime.Add(time.Hour)); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := repo.Update(ctx, updated, 1); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	stale := updated
	stale.Title = "stale write"
	if err := repo.Update(ctx, stale, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale Update() error = %v, want ErrConflict", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Status != procurement.StatusSubmitted || got.Title == "stale write" {
		t.Errorf("stored item was overwritten by stale update: %+v", got)
	}

	missing := newProcurement(t, "prc_missing", "PR-202610-9999", "Missing procurement", "Engineering", baseTime)
	if err := repo.Update(ctx, missing, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update(missing) error = %v, want ErrNotFound", err)
	}
}

func TestRepositoryReturnsCopies(t *testing.T) {
	ctx := context.Background()
	repo := testsupport.NewProcurementRepository()
	p := newProcurement(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	got.Title = "mutated outside the repository"

	again, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if again.Title != p.Title {
		t.Errorf("Title = %q, want %q (repository leaked an internal copy)", again.Title, p.Title)
	}
}

func TestRepositoryContextCancellation(t *testing.T) {
	repo := testsupport.NewProcurementRepository()
	p := newProcurement(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := repo.Create(ctx, p); !errors.Is(err, context.Canceled) {
		t.Errorf("Create() error = %v, want context.Canceled", err)
	}
	if _, err := repo.GetByID(ctx, p.ID); !errors.Is(err, context.Canceled) {
		t.Errorf("GetByID() error = %v, want context.Canceled", err)
	}
	if err := repo.Update(ctx, p, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("Update() error = %v, want context.Canceled", err)
	}
	if _, err := repo.List(ctx, procurement.ListFilter{Page: 1, PageSize: 20}); !errors.Is(err, context.Canceled) {
		t.Errorf("List() error = %v, want context.Canceled", err)
	}
	if err := repo.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Ping() error = %v, want context.Canceled", err)
	}
}

func TestRepositoryPing(t *testing.T) {
	repo := testsupport.NewProcurementRepository()
	if err := repo.Ping(context.Background()); err != nil {
		t.Errorf("Ping() error = %v, want nil", err)
	}
}
