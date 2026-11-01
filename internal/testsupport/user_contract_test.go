package testsupport_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/user"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
)

func TestUserRepositoryContract(t *testing.T) {
	repositorytest.RunUsers(t, func(t *testing.T) user.Repository {
		return testsupport.NewUserRepository()
	})
}
