package repositorytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
)

var userBaseTime = time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC)

// NewUserRepository returns an empty account repository. It must isolate state
// between calls so each subtest starts from a clean slate.
type NewUserRepository func(t *testing.T) user.Repository

// RunUsers executes the account repository contract.
func RunUsers(t *testing.T, newRepo NewUserRepository) {
	t.Helper()

	t.Run("UpsertAndGetCaseInsensitive", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		account := user.User{
			ID:           "usr_admin",
			Email:        "Admin@Procure.Local",
			Name:         "Admin",
			PasswordHash: "hash-1",
			Roles:        []string{"admin"},
			Active:       true,
			CreatedAt:    userBaseTime,
			UpdatedAt:    userBaseTime,
		}
		if err := repo.Upsert(ctx, account); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}

		stored, err := repo.GetByEmail(ctx, "admin@procure.local")
		if err != nil {
			t.Fatalf("GetByEmail() error = %v", err)
		}
		if stored.ID != account.ID {
			t.Errorf("ID = %q, want %q", stored.ID, account.ID)
		}
		if stored.Email != "admin@procure.local" {
			t.Errorf("Email = %q, want lower-cased address", stored.Email)
		}
		if len(stored.Roles) != 1 || stored.Roles[0] != "admin" {
			t.Errorf("Roles = %v, want [admin]", stored.Roles)
		}

		byID, err := repo.GetByID(ctx, account.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if byID.Email != "admin@procure.local" {
			t.Errorf("GetByID().Email = %q, want lower-cased address", byID.Email)
		}
	})

	t.Run("UpsertKeepsIdentityOnEmailConflict", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		original := user.User{
			ID:           "usr_requester",
			Email:        "requester@procure.local",
			Name:         "Old Name",
			PasswordHash: "hash-old",
			Roles:        []string{"requester"},
			Department:   "Engineering",
			Active:       true,
			CreatedAt:    userBaseTime,
			UpdatedAt:    userBaseTime,
		}
		if err := repo.Upsert(ctx, original); err != nil {
			t.Fatalf("seed Upsert() error = %v", err)
		}

		updated := original
		updated.ID = "usr_replacement"
		updated.Name = "New Name"
		updated.PasswordHash = "hash-new"
		updated.Roles = []string{"approver"}
		updated.Department = "Finance"
		updated.UpdatedAt = userBaseTime.Add(time.Hour)
		if err := repo.Upsert(ctx, updated); err != nil {
			t.Fatalf("second Upsert() error = %v", err)
		}

		stored, err := repo.GetByID(ctx, original.ID)
		if err != nil {
			t.Fatalf("GetByID(original) error = %v", err)
		}
		if stored.Name != "New Name" || stored.PasswordHash != "hash-new" {
			t.Errorf("stored = %q/%q, want updated name and hash", stored.Name, stored.PasswordHash)
		}
		if !stored.CreatedAt.Equal(userBaseTime) {
			t.Errorf("CreatedAt = %v, want preserved %v", stored.CreatedAt, userBaseTime)
		}
		if !stored.UpdatedAt.Equal(userBaseTime.Add(time.Hour)) {
			t.Errorf("UpdatedAt = %v, want %v", stored.UpdatedAt, userBaseTime.Add(time.Hour))
		}
		if !stored.Active {
			t.Error("Active = false, want true")
		}
		if len(stored.Roles) != 1 || stored.Roles[0] != "approver" {
			t.Errorf("Roles = %v, want [approver]", stored.Roles)
		}

		if _, err := repo.GetByID(ctx, "usr_replacement"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("GetByID(replacement) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("GetMissingAccount", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		if _, err := repo.GetByEmail(ctx, "nobody@procure.local"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("GetByEmail() error = %v, want ErrNotFound", err)
		}
		if _, err := repo.GetByID(ctx, "usr_missing"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("GetByID() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("UpsertRejectsIDReuseForAnotherEmail", func(t *testing.T) {
		repo := newRepo(t)
		ctx := context.Background()

		first := user.User{
			ID:           "usr_shared",
			Email:        "first@procure.local",
			Name:         "First",
			PasswordHash: "hash",
			Roles:        []string{"viewer"},
			Active:       true,
			CreatedAt:    userBaseTime,
			UpdatedAt:    userBaseTime,
		}
		if err := repo.Upsert(ctx, first); err != nil {
			t.Fatalf("first Upsert() error = %v", err)
		}

		second := first
		second.Email = "second@procure.local"
		if err := repo.Upsert(ctx, second); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("second Upsert() error = %v, want ErrConflict", err)
		}
	})
}
