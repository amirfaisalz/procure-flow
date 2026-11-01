package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
)

const userColumns = `id, email, name, password_hash, roles, department, active, created_at, updated_at`

const upsertUserSQL = `
INSERT INTO users (` + userColumns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (email) DO UPDATE SET
	name          = EXCLUDED.name,
	password_hash = EXCLUDED.password_hash,
	roles         = EXCLUDED.roles,
	department    = EXCLUDED.department,
	active        = EXCLUDED.active,
	updated_at    = EXCLUDED.updated_at`

// UserRepository is the PostgreSQL adapter for the account port.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a repository backed by db.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// GetByEmail returns the account for email, case insensitively.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (user.User, error) {
	row := querierFrom(ctx, r.db).QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = $1`, user.NormalizeEmail(email))

	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return user.User{}, domain.ErrNotFound
	}
	if err != nil {
		return user.User{}, fmt.Errorf("postgres: get user by email: %w", err)
	}
	return u, nil
}

// GetByID returns the account with the given identifier.
func (r *UserRepository) GetByID(ctx context.Context, id string) (user.User, error) {
	row := querierFrom(ctx, r.db).QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id)

	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return user.User{}, domain.ErrNotFound
	}
	if err != nil {
		return user.User{}, fmt.Errorf("postgres: get user by id: %w", err)
	}
	return u, nil
}

// Upsert stores u. The email is the conflict key: an existing account keeps its
// ID and creation time while the remaining fields are replaced.
func (r *UserRepository) Upsert(ctx context.Context, u user.User) error {
	roles, err := json.Marshal(u.Roles)
	if err != nil {
		return fmt.Errorf("postgres: encode user roles: %w", err)
	}

	_, err = querierFrom(ctx, r.db).ExecContext(ctx, upsertUserSQL,
		u.ID, user.NormalizeEmail(u.Email), u.Name, u.PasswordHash, string(roles), u.Department,
		u.Active, u.CreatedAt.UTC(), u.UpdatedAt.UTC(),
	)
	return mapWriteError(err)
}

func scanUser(row interface {
	Scan(dest ...any) error
}) (user.User, error) {
	var (
		u    user.User
		role []byte
	)
	if err := row.Scan(
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &role, &u.Department,
		&u.Active, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return user.User{}, err
	}
	if err := json.Unmarshal(role, &u.Roles); err != nil {
		return user.User{}, fmt.Errorf("decode roles for %s: %w", u.ID, err)
	}
	u.Email = user.NormalizeEmail(u.Email)
	u.CreatedAt = u.CreatedAt.UTC()
	u.UpdatedAt = u.UpdatedAt.UTC()
	return u, nil
}
