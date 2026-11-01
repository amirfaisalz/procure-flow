package procurement

import (
	"context"
	"strings"
)

// Roles understood by the procurement policy. They mirror the authentication
// roles but live here so the application layer stays independent of transport
// and credential concerns.
const (
	RoleAdmin     = "admin"
	RoleApprover  = "approver"
	RoleRequester = "requester"
	RoleViewer    = "viewer"
)

// Actor is the authenticated caller performing a use case.
type Actor struct {
	Subject    string
	Name       string
	Roles      []string
	Department string
}

// HasRole reports whether the actor holds role.
func (a Actor) HasRole(role string) bool {
	for _, held := range a.Roles {
		if strings.EqualFold(held, role) {
			return true
		}
	}
	return false
}

// IsAdmin reports whether the actor holds the admin role.
func (a Actor) IsAdmin() bool {
	return a.HasRole(RoleAdmin)
}

// DisplayName returns the actor's name, falling back to the subject.
func (a Actor) DisplayName() string {
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	return a.Subject
}

type actorKey struct{}

// WithActor stores the authenticated actor in ctx. The authentication
// middleware is responsible for populating it.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFrom returns the actor stored in ctx.
func ActorFrom(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(Actor)
	if !ok || strings.TrimSpace(actor.Subject) == "" {
		return Actor{}, false
	}
	return actor, true
}
