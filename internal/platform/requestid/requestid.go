package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

type contextKey struct{}

// New returns a 128-bit random request identifier encoded as hex.
func New() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failures are extremely rare; keep the request path alive.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf[:])
}

// With stores a request ID in the context.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// From returns the request ID stored in the context, or an empty string.
func From(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
