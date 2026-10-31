// Package repositorytest contains the behavioural contract that every
// procurement repository adapter must satisfy. Adapters call Run from their own
// test files with a factory that returns an isolated repository.
package repositorytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

var baseTime = time.Date(2026, time.October, 31, 10, 0, 0, 0, time.UTC)

// NewRepository returns an empty repository. It must isolate state between
// calls so each subtest starts from a clean slate.
type NewRepository func(t *testing.T) procurement.Repository

// Run executes the full repository contract.
func Run(t *testing.T, newRepo NewRepository) {
	t.Helper()

	t.Run("CreateAndGet", func(t *testing.T) {
		repo := newRepo(t)
		want := mustNew(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)

		if err := repo.Create(context.Background(), want); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, err := repo.GetByID(context.Background(), want.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		assertProcurementEqual(t, got, want)
	})

	t.Run("CreateDuplicateID", func(t *testing.T) {
		repo := newRepo(t)
		first := mustNew(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)
		second := mustNew(t, "prc_1", "PR-202610-0002", "Office chairs", "Facilities", baseTime)

		if err := repo.Create(context.Background(), first); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if err := repo.Create(context.Background(), second); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("Create() duplicate id error = %v, want ErrConflict", err)
		}
	})

	t.Run("CreateDuplicateNumber", func(t *testing.T) {
		repo := newRepo(t)
		first := mustNew(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)
		second := mustNew(t, "prc_2", "PR-202610-0001", "Office chairs", "Facilities", baseTime)

		if err := repo.Create(context.Background(), first); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if err := repo.Create(context.Background(), second); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("Create() duplicate number error = %v, want ErrConflict", err)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		repo := newRepo(t)
		if _, err := repo.GetByID(context.Background(), "prc_missing"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("GetByID() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		repo := newRepo(t)
		original := mustNew(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)
		if err := repo.Create(context.Background(), original); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		updated := original
		updated.Title = "Laptop procurement (updated)"
		updated.Status = procurement.StatusSubmitted
		updated.UpdatedAt = baseTime.Add(time.Hour)
		updated.Version = original.Version + 1

		if err := repo.Update(context.Background(), updated, original.Version); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		got, err := repo.GetByID(context.Background(), updated.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		assertProcurementEqual(t, got, updated)
	})

	t.Run("UpdateStaleVersion", func(t *testing.T) {
		repo := newRepo(t)
		original := mustNew(t, "prc_1", "PR-202610-0001", "Laptop procurement", "Engineering", baseTime)
		if err := repo.Create(context.Background(), original); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		first := original
		first.Title = "First update"
		first.Version = original.Version + 1
		if err := repo.Update(context.Background(), first, original.Version); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		stale := original
		stale.Title = "Stale update"
		stale.Version = original.Version + 1
		if err := repo.Update(context.Background(), stale, original.Version); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("Update() stale error = %v, want ErrConflict", err)
		}

		got, err := repo.GetByID(context.Background(), original.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if got.Title != first.Title || got.Version != first.Version {
			t.Errorf("stored = (%q, v%d), want (%q, v%d)", got.Title, got.Version, first.Title, first.Version)
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		repo := newRepo(t)
		missing := mustNew(t, "prc_missing", "PR-202610-9999", "Missing procurement", "Engineering", baseTime)
		if err := repo.Update(context.Background(), missing, missing.Version); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Update() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("ListFilterAndPagination", func(t *testing.T) {
		repo := newRepo(t)
		seedList(t, repo)

		all, err := repo.List(context.Background(), procurement.ListFilter{Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if all.Total != 5 {
			t.Errorf("Total = %d, want 5", all.Total)
		}
		if len(all.Items) != 5 {
			t.Fatalf("len(Items) = %d, want 5", len(all.Items))
		}
		if all.Items[0].ID != "prc_5" {
			t.Errorf("first item = %q, want prc_5 (newest first)", all.Items[0].ID)
		}

		page, err := repo.List(context.Background(), procurement.ListFilter{Page: 2, PageSize: 2})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if page.Total != 5 {
			t.Errorf("Total = %d, want 5", page.Total)
		}
		if len(page.Items) != 2 || page.Items[0].ID != "prc_3" || page.Items[1].ID != "prc_2" {
			t.Errorf("page 2 = %v, want [prc_3 prc_2]", ids(page.Items))
		}

		approved, err := repo.List(context.Background(), procurement.ListFilter{
			Status: procurement.StatusApproved, Page: 1, PageSize: 10,
		})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if approved.Total != 1 || approved.Items[0].ID != "prc_1" {
			t.Errorf("approved = %v, want [prc_1]", ids(approved.Items))
		}
	})

	t.Run("ListSearch", func(t *testing.T) {
		repo := newRepo(t)
		seedList(t, repo)

		tests := []struct {
			query string
			want  []string
		}{
			{query: "laptop", want: []string{"prc_5", "prc_3", "prc_1"}},
			{query: "FACILITIES", want: []string{"prc_2"}},
			{query: "PR-202610-0004", want: []string{"prc_4"}},
			{query: "nothing-matches", want: nil},
		}

		for _, tt := range tests {
			t.Run(tt.query, func(t *testing.T) {
				result, err := repo.List(context.Background(), procurement.ListFilter{
					Query: tt.query, Page: 1, PageSize: 10,
				})
				if err != nil {
					t.Fatalf("List() error = %v", err)
				}
				if got := ids(result.Items); !equalStrings(got, tt.want) {
					t.Errorf("ids = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("ListEmpty", func(t *testing.T) {
		repo := newRepo(t)
		result, err := repo.List(context.Background(), procurement.ListFilter{Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if result.Total != 0 || len(result.Items) != 0 {
			t.Errorf("result = (%d, %d), want (0, 0)", result.Total, len(result.Items))
		}
		if result.Items == nil {
			t.Error("Items = nil, want an empty slice for JSON encoding")
		}
	})
}

func seedList(t *testing.T, repo procurement.Repository) {
	t.Helper()

	// prc_1 approved, prc_2 submitted, prc_3..prc_5 draft. Created in ascending
	// time order so "newest first" means prc_5, prc_4, prc_3, prc_2, prc_1.
	items := []struct {
		id         string
		number     string
		title      string
		department string
		status     procurement.Status
		offset     time.Duration
	}{
		{"prc_1", "PR-202610-0001", "Laptop engineering", "Engineering", procurement.StatusApproved, time.Minute},
		{"prc_2", "PR-202610-0002", "Office chairs", "Facilities", procurement.StatusSubmitted, 2 * time.Minute},
		{"prc_3", "PR-202610-0003", "Laptop support", "Support", procurement.StatusDraft, 3 * time.Minute},
		{"prc_4", "PR-202610-0004", "Server rack", "Engineering", procurement.StatusDraft, 4 * time.Minute},
		{"prc_5", "PR-202610-0005", "Marketing laptop campaign", "Marketing", procurement.StatusDraft, 5 * time.Minute},
	}

	for _, item := range items {
		p := mustNew(t, item.id, item.number, item.title, item.department, baseTime.Add(item.offset))
		switch item.status {
		case procurement.StatusSubmitted:
			if err := p.Submit(baseTime.Add(item.offset)); err != nil {
				t.Fatalf("Submit() error = %v", err)
			}
		case procurement.StatusApproved:
			if err := p.Submit(baseTime.Add(item.offset)); err != nil {
				t.Fatalf("Submit() error = %v", err)
			}
			if err := p.Approve(baseTime.Add(item.offset)); err != nil {
				t.Fatalf("Approve() error = %v", err)
			}
		}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatalf("Create(%s) error = %v", item.id, err)
		}
	}
}

func mustNew(t *testing.T, id, number, title, department string, now time.Time) procurement.Procurement {
	t.Helper()

	p, err := procurement.New(id, number, procurement.CreateParams{
		Title:       title,
		Description: "seed description",
		Department:  department,
		Requester:   "Amir",
		RequesterID: "user_amir",
		Amount:      250_000_000,
		Currency:    "IDR",
	}, now)
	if err != nil {
		t.Fatalf("procurement.New() error = %v", err)
	}
	return p
}

func assertProcurementEqual(t *testing.T, got, want procurement.Procurement) {
	t.Helper()

	if got.ID != want.ID ||
		got.Number != want.Number ||
		got.Title != want.Title ||
		got.Description != want.Description ||
		got.Department != want.Department ||
		got.Requester != want.Requester ||
		got.RequesterID != want.RequesterID ||
		got.Amount != want.Amount ||
		got.Currency != want.Currency ||
		got.Status != want.Status ||
		got.RejectionReason != want.RejectionReason ||
		got.Version != want.Version ||
		!got.CreatedAt.Equal(want.CreatedAt) ||
		!got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("procurement mismatch:\n got = %+v\nwant = %+v", got, want)
	}
}

func ids(items []procurement.Procurement) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
