package auth_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	authapp "github.com/amirfaisalz/procure-flow/internal/application/auth"
	"github.com/amirfaisalz/procure-flow/internal/platform/password"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
)

func TestSeedDemoAccounts(t *testing.T) {
	repo := testsupport.NewUserRepository()
	hasher := password.New(bcrypt.MinCost)
	counter := 0
	newID := func() string {
		counter++
		return fmt.Sprintf("usr_%d", counter)
	}

	seeded, err := authapp.SeedDemoAccounts(context.Background(), repo, hasher, newID, authapp.DemoPassword, time.Now)
	if err != nil {
		t.Fatalf("SeedDemoAccounts() error = %v", err)
	}
	if len(seeded) != len(authapp.DemoAccounts()) {
		t.Fatalf("seeded %d accounts, want %d", len(seeded), len(authapp.DemoAccounts()))
	}

	admin, err := repo.GetByEmail(context.Background(), "admin@procure.local")
	if err != nil {
		t.Fatalf("GetByEmail() error = %v", err)
	}
	if len(admin.Roles) != 1 || admin.Roles[0] != "admin" {
		t.Errorf("admin roles = %v, want [admin]", admin.Roles)
	}
	if err := hasher.Verify(admin.PasswordHash, authapp.DemoPassword); err != nil {
		t.Errorf("seeded password does not verify: %v", err)
	}
	if !admin.Active {
		t.Error("seeded account is inactive")
	}

	// Re-running rotates the password but keeps the account count stable.
	if _, err := authapp.SeedDemoAccounts(context.Background(), repo, hasher, newID, authapp.DemoPassword, time.Now); err != nil {
		t.Fatalf("second SeedDemoAccounts() error = %v", err)
	}
	again, err := repo.GetByEmail(context.Background(), "admin@procure.local")
	if err != nil {
		t.Fatalf("GetByEmail() after reseed error = %v", err)
	}
	if again.ID != admin.ID {
		t.Errorf("account ID changed from %q to %q; seeding must be idempotent", admin.ID, again.ID)
	}
}
