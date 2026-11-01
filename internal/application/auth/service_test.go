package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	authapp "github.com/amirfaisalz/procure-flow/internal/application/auth"
	"github.com/amirfaisalz/procure-flow/internal/domain/user"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/platform/password"
)

var sessionExpiry = time.Date(2026, time.November, 1, 17, 0, 0, 0, time.UTC)

type fakeIssuer struct {
	called                           bool
	subject, name, email, department string
	roles                            []string
	ttl                              time.Duration
	err                              error
}

func (f *fakeIssuer) Issue(
	subject, name, email string,
	roles []string,
	department string,
	ttl time.Duration,
) (string, time.Time, error) {
	f.called = true
	f.subject, f.name, f.email, f.department = subject, name, email, department
	f.roles, f.ttl = roles, ttl
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	return "token-" + subject, sessionExpiry, nil
}

func seedAccount(t *testing.T, repo *memory.UserRepository, email, plain string, active bool, roles ...string) user.User {
	t.Helper()

	hash, err := password.New(bcrypt.MinCost).Hash(plain)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	account := user.User{
		ID:           "usr_" + email,
		Email:        email,
		Name:         "Test Account",
		PasswordHash: hash,
		Roles:        roles,
		Department:   "Engineering",
		Active:       active,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := repo.Upsert(context.Background(), account); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return account
}

func TestLoginIssuesSession(t *testing.T) {
	repo := memory.NewUserRepository()
	account := seedAccount(t, repo, "Approver@Procure.Local", "correct-horse", true, "approver")

	issuer := &fakeIssuer{}
	service := authapp.NewService(repo, password.New(bcrypt.MinCost), issuer, 4*time.Hour)

	session, err := service.Login(context.Background(), "approver@procure.local", "correct-horse")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if session.Token != "token-"+account.ID {
		t.Errorf("Token = %q, want token for %q", session.Token, account.ID)
	}
	if !session.ExpiresAt.Equal(sessionExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", session.ExpiresAt, sessionExpiry)
	}
	if !issuer.called {
		t.Fatal("issuer was not called")
	}
	if issuer.roles[0] != "approver" || issuer.department != "Engineering" {
		t.Errorf("issuer claims roles=%v department=%q, want approver/Engineering", issuer.roles, issuer.department)
	}
	if issuer.ttl != 4*time.Hour {
		t.Errorf("issuer ttl = %v, want 4h", issuer.ttl)
	}
	if session.User.Email != "approver@procure.local" {
		t.Errorf("User.Email = %q, want normalized email", session.User.Email)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	repo := memory.NewUserRepository()
	seedAccount(t, repo, "requester@procure.local", "correct-horse", true, "requester")

	service := authapp.NewService(repo, password.New(bcrypt.MinCost), &fakeIssuer{}, time.Hour)

	tests := []struct {
		name     string
		email    string
		password string
	}{
		{name: "wrong password", email: "requester@procure.local", password: "wrong-horse"},
		{name: "unknown email", email: "nobody@procure.local", password: "correct-horse"},
		{name: "empty email", email: "", password: "correct-horse"},
		{name: "empty password", email: "requester@procure.local", password: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.Login(context.Background(), test.email, test.password); !errors.Is(err, authapp.ErrInvalidCredentials) {
				t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestLoginRejectsInactiveAccount(t *testing.T) {
	repo := memory.NewUserRepository()
	seedAccount(t, repo, "disabled@procure.local", "correct-horse", false, "viewer")

	service := authapp.NewService(repo, password.New(bcrypt.MinCost), &fakeIssuer{}, time.Hour)

	if _, err := service.Login(context.Background(), "disabled@procure.local", "correct-horse"); !errors.Is(err, authapp.ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

type failingRepository struct{}

func (failingRepository) GetByEmail(context.Context, string) (user.User, error) {
	return user.User{}, errors.New("storage unavailable")
}
func (failingRepository) GetByID(context.Context, string) (user.User, error) {
	return user.User{}, errors.New("storage unavailable")
}
func (failingRepository) Upsert(context.Context, user.User) error {
	return errors.New("storage unavailable")
}

func TestLoginPropagatesRepositoryFailure(t *testing.T) {
	service := authapp.NewService(failingRepository{}, password.New(bcrypt.MinCost), &fakeIssuer{}, time.Hour)

	_, err := service.Login(context.Background(), "admin@procure.local", "correct-horse")
	if err == nil || errors.Is(err, authapp.ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want a storage error", err)
	}
}

func TestLoginFailsWhenIssuerFails(t *testing.T) {
	repo := memory.NewUserRepository()
	seedAccount(t, repo, "admin@procure.local", "correct-horse", true, "admin")

	issuer := &fakeIssuer{err: errors.New("signing key unavailable")}
	service := authapp.NewService(repo, password.New(bcrypt.MinCost), issuer, time.Hour)

	if _, err := service.Login(context.Background(), "admin@procure.local", "correct-horse"); err == nil {
		t.Fatal("Login() error = nil, want issuer failure")
	}
}

func TestDefaultSessionTTL(t *testing.T) {
	service := authapp.NewService(memory.NewUserRepository(), password.New(bcrypt.MinCost), &fakeIssuer{}, 0)
	if service.TTL() != authapp.DefaultSessionTTL {
		t.Errorf("TTL() = %v, want %v", service.TTL(), authapp.DefaultSessionTTL)
	}
}
