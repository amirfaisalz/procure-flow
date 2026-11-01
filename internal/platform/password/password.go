// Package password hashes and verifies account passwords with bcrypt.
package password

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultCost is the bcrypt work factor used for stored passwords.
	DefaultCost = 12
	// MinLength is the shortest password accepted when hashing a new one.
	MinLength = 8
	// MaxBytes is bcrypt's input limit; longer inputs are rejected instead of
	// silently truncated.
	MaxBytes = 72
)

// Hasher hashes passwords at a fixed bcrypt cost.
type Hasher struct {
	cost      int
	dummyOnce sync.Once
	dummy     string
}

// New returns a hasher with the given cost, clamped to bcrypt's supported range.
func New(cost int) *Hasher {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = DefaultCost
	}
	return &Hasher{cost: cost}
}

// NewDefault returns a hasher with DefaultCost.
func NewDefault() *Hasher {
	return New(DefaultCost)
}

// Cost reports the configured work factor.
func (h *Hasher) Cost() int {
	return h.cost
}

// Hash validates and hashes a plaintext password.
func (h *Hasher) Hash(plain string) (string, error) {
	if err := Validate(plain); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", fmt.Errorf("password: hash: %w", err)
	}
	return string(hash), nil
}

// Verify reports whether plain matches the stored hash.
func (h *Hasher) Verify(hash, plain string) error {
	if strings.TrimSpace(hash) == "" {
		return fmt.Errorf("password: empty hash")
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}

// DummyHash returns a hash of a random throwaway value. Login verifies against
// it when the account does not exist, so unknown emails cost roughly the same
// as wrong passwords and cannot be enumerated by timing.
func (h *Hasher) DummyHash() string {
	h.dummyOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), h.cost)
		if err != nil {
			// Fall back to a syntactically valid hash; Verify will simply fail.
			h.dummy = "$2a$12$C6UzMDM.H6dfI/f/IKcEe.9mF3r5fVnTQJ0dQ0m9G2xY8a1h6o3Wa"
			return
		}
		h.dummy = string(hash)
	})
	return h.dummy
}

// Validate enforces the password policy applied when a password is set.
func Validate(plain string) error {
	switch {
	case len(plain) < MinLength:
		return fmt.Errorf("password: must be at least %d characters", MinLength)
	case len(plain) > MaxBytes:
		return fmt.Errorf("password: must not exceed %d bytes", MaxBytes)
	}
	return nil
}
