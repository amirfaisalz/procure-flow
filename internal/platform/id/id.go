package id

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Generator creates opaque entity identifiers and human-readable request numbers.
type Generator struct{}

// NewGenerator returns a stateless generator.
func NewGenerator() *Generator {
	return &Generator{}
}

// NewID returns an opaque identifier such as prc_9f2c... .
func (Generator) NewID() string {
	return "prc_" + randomHex(16)
}

// NewUserID returns an account identifier such as usr_9f2c... .
func (Generator) NewUserID() string {
	return "usr_" + randomHex(16)
}

// NewNumber returns a request number such as PR-202610-A1B2C3D4E5F60718.
// The suffix carries 64 bits of entropy so collisions stay negligible even at
// high monthly volumes. The database additionally enforces uniqueness.
func (Generator) NewNumber(now time.Time) string {
	return fmt.Sprintf("PR-%s-%s", now.UTC().Format("200601"), strings.ToUpper(randomHex(8)))
}

var fallbackCounter atomic.Uint64

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}

	sum := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), fallbackCounter.Add(1))))
	return hex.EncodeToString(sum[:n])
}
