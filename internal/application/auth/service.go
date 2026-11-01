// Package auth implements account login and session issuance.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
)

// DefaultSessionTTL is used when no TTL is configured.
const DefaultSessionTTL = 8 * time.Hour

// ErrInvalidCredentials is returned for unknown accounts, wrong passwords, and
// disabled accounts alike, so callers cannot probe which one applies.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// Hasher verifies passwords and provides a decoy hash for unknown accounts.
type Hasher interface {
	Verify(hash, plain string) error
	DummyHash() string
}

// SessionIssuer signs a session token for an account.
type SessionIssuer interface {
	Issue(subject, name, email string, roles []string, department string, ttl time.Duration) (string, time.Time, error)
}

// Session is the result of a successful login.
type Session struct {
	User      user.User
	Token     string
	ExpiresAt time.Time
}

// Service implements the login use case.
type Service struct {
	users  user.Repository
	hasher Hasher
	issuer SessionIssuer
	ttl    time.Duration
}

// NewService wires the login collaborators.
func NewService(users user.Repository, hasher Hasher, issuer SessionIssuer, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &Service{users: users, hasher: hasher, issuer: issuer, ttl: ttl}
}

// TTL reports the configured session lifetime.
func (s *Service) TTL() time.Duration {
	return s.ttl
}

// Login verifies the credentials, then signs a session token carrying the
// account's roles and department.
//
// Unknown and inactive accounts still run a password comparison against a decoy
// hash so their timing does not reveal whether the email exists.
func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	email = user.NormalizeEmail(email)
	if email == "" || password == "" {
		return Session{}, ErrInvalidCredentials
	}

	account, err := s.users.GetByEmail(ctx, email)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		_ = s.hasher.Verify(s.hasher.DummyHash(), password)
		return Session{}, ErrInvalidCredentials
	case err != nil:
		return Session{}, fmt.Errorf("auth: load account: %w", err)
	}

	if !account.Active {
		_ = s.hasher.Verify(account.PasswordHash, password)
		return Session{}, ErrInvalidCredentials
	}

	if err := s.hasher.Verify(account.PasswordHash, password); err != nil {
		return Session{}, ErrInvalidCredentials
	}

	token, expiresAt, err := s.issuer.Issue(
		account.ID, account.Name, account.Email, account.Roles, account.Department, s.ttl,
	)
	if err != nil {
		return Session{}, fmt.Errorf("auth: issue session: %w", err)
	}

	return Session{User: account.Clone(), Token: token, ExpiresAt: expiresAt}, nil
}
