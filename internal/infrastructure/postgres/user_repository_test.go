package postgres_test

import (
	"context"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/user"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/postgres"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestUserRepositoryContract(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	repositorytest.RunUsers(t, func(t *testing.T) user.Repository {
		t.Helper()
		if _, err := db.ExecContext(ctx, "TRUNCATE users"); err != nil {
			t.Fatalf("truncate users: %v", err)
		}
		return postgres.NewUserRepository(db)
	})
}
