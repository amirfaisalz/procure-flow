package auth

import (
	"errors"
	"fmt"
	"strings"
)

// Role is an application role.
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleApprover  Role = "approver"
	RoleRequester Role = "requester"
	RoleViewer    Role = "viewer"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleApprover, RoleRequester, RoleViewer:
		return true
	default:
		return false
	}
}

// ParseRoles normalises and validates a list of role names. At least one valid
// role is required.
func ParseRoles(raw []string) ([]Role, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("auth: at least one role is required")
	}

	roles := make([]Role, 0, len(raw))
	seen := make(map[Role]struct{}, len(raw))
	for _, value := range raw {
		role := Role(strings.ToLower(strings.TrimSpace(value)))
		if !role.Valid() {
			return nil, fmt.Errorf("auth: unknown role %q", value)
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	return roles, nil
}

// Principal is the authenticated caller.
type Principal struct {
	Subject    string
	Name       string
	Roles      []Role
	Department string
}

// HasRole reports whether the principal holds role.
func (p Principal) HasRole(role Role) bool {
	for _, held := range p.Roles {
		if held == role {
			return true
		}
	}
	return false
}

// ErrUnauthenticated is returned when a request carries no valid credentials.
var ErrUnauthenticated = errors.New("unauthenticated")
