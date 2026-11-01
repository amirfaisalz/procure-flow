package testsupport_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/outbox"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
)

func TestOutboxRepositoryContract(t *testing.T) {
	repositorytest.RunOutbox(t, func(t *testing.T) outbox.Repository {
		t.Helper()
		return testsupport.NewOutboxRepository()
	})
}
