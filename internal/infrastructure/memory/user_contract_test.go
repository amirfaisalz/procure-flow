package memory_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/user"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestUserRepositoryContract(t *testing.T) {
	repositorytest.RunUsers(t, func(t *testing.T) user.Repository {
		return memory.NewUserRepository()
	})
}
