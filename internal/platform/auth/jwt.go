package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const maxTokenLength = 8 << 10

// JWTConfig configures HS256 verification.
type JWTConfig struct {
	Secret   []byte
	Issuer   string
	Audience string
	Leeway   time.Duration
	// CookieName, when set, makes the authenticator accept the token from that
	// cookie as well as from the Authorization header. Header credentials win.
	CookieName string
}

// JWTAuthenticator verifies HS256 bearer tokens issued by a trusted party.
//
// Only HS256 is accepted: the algorithm is pinned, which prevents "alg: none"
// and algorithm-confusion attacks.
type JWTAuthenticator struct {
	config JWTConfig
	now    func() time.Time
}

// NewJWTAuthenticator copies cfg into a new authenticator.
func NewJWTAuthenticator(cfg JWTConfig) *JWTAuthenticator {
	secret := make([]byte, len(cfg.Secret))
	copy(secret, cfg.Secret)
	cfg.Secret = secret
	return &JWTAuthenticator{config: cfg, now: time.Now}
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	Subject    string   `json:"sub"`
	Name       string   `json:"name"`
	Email      string   `json:"email"`
	Roles      []string `json:"roles"`
	Department string   `json:"department"`
	Issuer     string   `json:"iss"`
	Audience   string   `json:"aud"`
	IssuedAt   int64    `json:"iat"`
	ExpiresAt  int64    `json:"exp"`
	NotBefore  int64    `json:"nbf"`
}

// Authenticate implements Authenticator.
func (a *JWTAuthenticator) Authenticate(_ context.Context, r *http.Request) (Principal, error) {
	token := tokenFromRequest(r, a.config.CookieName)
	if token == "" || len(token) > maxTokenLength {
		return Principal{}, ErrUnauthenticated
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, ErrUnauthenticated
	}

	headerJSON, err := decodeSegment(parts[0])
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil || header.Alg != "HS256" {
		return Principal{}, ErrUnauthenticated
	}
	if header.Typ != "" && !strings.EqualFold(header.Typ, "JWT") {
		return Principal{}, ErrUnauthenticated
	}

	mac := hmac.New(sha256.New, a.config.Secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)

	signature, err := decodeSegment(parts[2])
	if err != nil || !hmac.Equal(expected, signature) {
		return Principal{}, ErrUnauthenticated
	}

	payload, err := decodeSegment(parts[1])
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Principal{}, ErrUnauthenticated
	}

	if err := a.validate(claims); err != nil {
		return Principal{}, err
	}

	roles, err := ParseRoles(claims.Roles)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}

	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = claims.Subject
	}

	return Principal{
		Subject:    strings.TrimSpace(claims.Subject),
		Name:       name,
		Email:      strings.TrimSpace(claims.Email),
		Roles:      roles,
		Department: strings.TrimSpace(claims.Department),
	}, nil
}

func (a *JWTAuthenticator) validate(claims jwtClaims) error {
	now := a.now().UTC()

	if claims.ExpiresAt == 0 {
		return ErrUnauthenticated
	}
	if now.After(time.Unix(claims.ExpiresAt, 0).Add(a.config.Leeway)) {
		return ErrUnauthenticated
	}
	if claims.NotBefore != 0 && now.Add(a.config.Leeway).Before(time.Unix(claims.NotBefore, 0)) {
		return ErrUnauthenticated
	}
	if a.config.Issuer != "" && claims.Issuer != a.config.Issuer {
		return ErrUnauthenticated
	}
	if a.config.Audience != "" && claims.Audience != a.config.Audience {
		return ErrUnauthenticated
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return ErrUnauthenticated
	}
	return nil
}

func decodeSegment(segment string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(segment); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(segment)
}
