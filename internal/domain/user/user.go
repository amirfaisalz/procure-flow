// Package user defines the account aggregate used by the login endpoints.
package user

import (
	"strings"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain"
)

// User is a login-enabled account. Passwords are only ever stored as a hash.
type User struct {
	ID           string
	Email        string
	Name         string
	PasswordHash string
	Roles        []string
	Department   string
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NormalizeEmail trims and lower-cases an address so lookups are case
// insensitive and unique indexes cannot be defeated by casing.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Validate checks the fields required for persistence and login.
func (u User) Validate() error {
	fields := map[string]string{}

	if strings.TrimSpace(u.ID) == "" {
		fields["id"] = "is required"
	}
	email := NormalizeEmail(u.Email)
	if email == "" {
		fields["email"] = "is required"
	} else if strings.ContainsAny(email, " \t\r\n") || !strings.Contains(email, "@") {
		fields["email"] = "must be a valid email address"
	}
	if strings.TrimSpace(u.Name) == "" {
		fields["name"] = "is required"
	}
	if strings.TrimSpace(u.PasswordHash) == "" {
		fields["password_hash"] = "is required"
	}
	if len(u.Roles) == 0 {
		fields["roles"] = "at least one role is required"
	}

	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

// NormalizedEmail returns the canonical form of the account email.
func (u User) NormalizedEmail() string {
	return NormalizeEmail(u.Email)
}

// Clone returns a copy whose roles slice does not alias the original.
func (u User) Clone() User {
	u.Roles = append([]string(nil), u.Roles...)
	return u
}
