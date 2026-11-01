// Package idempotency defines the contract for replay-safe write requests.
package idempotency

import (
	"context"
	"time"
)

// Record is the stored outcome of one idempotent request. A zero Status means
// the request has been claimed but not completed yet.
type Record struct {
	ActorID     string
	Key         string
	RequestHash string
	Status      int
	ContentType string
	Location    string
	Body        []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// InProgress reports whether another request holds the claim.
func (r Record) InProgress() bool {
	return r.Status == 0
}

// Repository persists idempotency claims and their responses.
//
// Claim must be atomic: for a given (ActorID, Key) at most one concurrent
// caller may receive claimed=true. When the key already exists, Claim returns
// the stored record with claimed=false. An expired record may be reclaimed.
type Repository interface {
	Claim(ctx context.Context, record Record) (existing Record, claimed bool, err error)
	Complete(ctx context.Context, actorID, key string, status int, contentType, location string, body []byte) error
	Release(ctx context.Context, actorID, key string) error
}
