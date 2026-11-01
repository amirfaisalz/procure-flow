package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/user"
)

// DemoPassword is the development password used when SEED_PASSWORD is unset.
// Seeding refuses to use it in production.
const DemoPassword = "demo-password-123"

// DemoAccount describes a seedable account. The set covers every role so the
// full authorization matrix can be exercised from a fresh database.
type DemoAccount struct {
	Email      string
	Name       string
	Roles      []string
	Department string
}

// PasswordHasher hashes a plaintext password for a seeded account.
type PasswordHasher interface {
	Hash(plain string) (string, error)
}

// DemoAccounts returns the accounts created by cmd/seed, the compose seed
// service, and the in-memory development bootstrap. Emails use .local so they
// can never collide with a real domain.
func DemoAccounts() []DemoAccount {
	return []DemoAccount{
		{Email: "admin@procure.local", Name: "Local Admin", Roles: []string{"admin"}},
		{Email: "approver@procure.local", Name: "Finance Approver", Roles: []string{"approver"}, Department: "Engineering"},
		{Email: "requester@procure.local", Name: "Amir Requester", Roles: []string{"requester"}, Department: "Engineering"},
		{Email: "viewer@procure.local", Name: "Read Only", Roles: []string{"viewer"}},
	}
}

// SeedDemoAccounts upserts every demo account with the given password and
// returns the seeded records. It is idempotent because the repository keys
// accounts by email.
func SeedDemoAccounts(
	ctx context.Context,
	repo user.Repository,
	hasher PasswordHasher,
	newID func() string,
	plain string,
	now func() time.Time,
) ([]user.User, error) {
	accounts := DemoAccounts()
	seeded := make([]user.User, 0, len(accounts))

	for _, account := range accounts {
		hash, err := hasher.Hash(plain)
		if err != nil {
			return nil, fmt.Errorf("hash password for %s: %w", account.Email, err)
		}

		timestamp := now().UTC()
		record := user.User{
			ID:           newID(),
			Email:        account.Email,
			Name:         account.Name,
			PasswordHash: hash,
			Roles:        account.Roles,
			Department:   account.Department,
			Active:       true,
			CreatedAt:    timestamp,
			UpdatedAt:    timestamp,
		}
		if err := record.Validate(); err != nil {
			return nil, fmt.Errorf("account %s: %w", account.Email, err)
		}
		if err := repo.Upsert(ctx, record); err != nil {
			return nil, fmt.Errorf("seed %s: %w", account.Email, err)
		}
		seeded = append(seeded, record.Clone())
	}
	return seeded, nil
}
