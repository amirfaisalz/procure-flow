package memory

import (
	"context"
	"sync"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
)

// UserRepository is a thread-safe, in-memory account store for local
// development and tests.
type UserRepository struct {
	mu      sync.RWMutex
	byID    map[string]user.User
	byEmail map[string]string
}

// NewUserRepository creates an empty repository.
func NewUserRepository() *UserRepository {
	return &UserRepository{
		byID:    make(map[string]user.User),
		byEmail: make(map[string]string),
	}
}

// Ping reports whether the repository is reachable.
func (r *UserRepository) Ping(ctx context.Context) error {
	return ctx.Err()
}

// GetByEmail returns the account registered for email, case insensitively.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (user.User, error) {
	if err := ctx.Err(); err != nil {
		return user.User{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.byEmail[user.NormalizeEmail(email)]
	if !ok {
		return user.User{}, domain.ErrNotFound
	}
	return r.byID[id].Clone(), nil
}

// GetByID returns the account with the given identifier.
func (r *UserRepository) GetByID(ctx context.Context, id string) (user.User, error) {
	if err := ctx.Err(); err != nil {
		return user.User{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	u, ok := r.byID[id]
	if !ok {
		return user.User{}, domain.ErrNotFound
	}
	return u.Clone(), nil
}

// Upsert stores u. When the email already exists the account keeps its ID and
// creation time and the remaining fields are replaced.
func (r *UserRepository) Upsert(ctx context.Context, u user.User) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	email := user.NormalizeEmail(u.Email)
	u = u.Clone()
	u.Email = email

	r.mu.Lock()
	defer r.mu.Unlock()

	if id, ok := r.byEmail[email]; ok {
		stored := r.byID[id]
		u.ID = stored.ID
		u.CreatedAt = stored.CreatedAt
		r.byID[id] = u
		return nil
	}

	if other, ok := r.byID[u.ID]; ok && other.NormalizedEmail() != email {
		return domain.ErrConflict
	}
	r.byID[u.ID] = u
	r.byEmail[email] = u.ID
	return nil
}
