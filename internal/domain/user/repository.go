package user

import "context"

// Repository stores login accounts. Email is the natural key: Upsert updates
// the account that already owns the email and preserves its ID and creation
// time, which makes seeding idempotent.
type Repository interface {
	GetByEmail(ctx context.Context, email string) (User, error)
	GetByID(ctx context.Context, id string) (User, error)
	Upsert(ctx context.Context, u User) error
}
