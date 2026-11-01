package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// JWTIssuer signs HS256 session tokens. The claim set is deliberately the same
// one JWTAuthenticator verifies, so a token minted here is immediately usable as
// a bearer credential or as the session cookie.
type JWTIssuer struct {
	secret   []byte
	issuer   string
	audience string
	now      func() time.Time
}

// NewJWTIssuer creates an issuer. The caller is responsible for keeping the
// secret long enough; configuration enforces a minimum length.
func NewJWTIssuer(secret []byte, issuer, audience string) *JWTIssuer {
	copied := make([]byte, len(secret))
	copy(copied, secret)
	return &JWTIssuer{
		secret:   copied,
		issuer:   issuer,
		audience: audience,
		now:      time.Now,
	}
}

// Issue signs a token for the principal and returns it with its expiry.
func (i *JWTIssuer) Issue(
	subject, name, email string,
	roles []string,
	department string,
	ttl time.Duration,
) (string, time.Time, error) {
	if ttl <= 0 {
		return "", time.Time{}, fmt.Errorf("auth: token ttl must be positive")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", time.Time{}, fmt.Errorf("auth: subject is required")
	}

	parsed, err := ParseRoles(roles)
	if err != nil {
		return "", time.Time{}, err
	}
	normalized := make([]string, len(parsed))
	for idx, role := range parsed {
		normalized[idx] = string(role)
	}

	now := i.now().UTC().Truncate(time.Second)
	expiresAt := now.Add(ttl)

	headerJSON, err := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: encode header: %w", err)
	}
	claimsJSON, err := json.Marshal(jwtClaims{
		Subject:    subject,
		Name:       strings.TrimSpace(name),
		Email:      strings.TrimSpace(email),
		Roles:      normalized,
		Department: strings.TrimSpace(department),
		Issuer:     i.issuer,
		Audience:   i.audience,
		IssuedAt:   now.Unix(),
		NotBefore:  now.Unix(),
		ExpiresAt:  expiresAt.Unix(),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: encode claims: %w", err)
	}

	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + payload

	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + signature, expiresAt, nil
}
