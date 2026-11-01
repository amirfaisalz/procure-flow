package memory_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/outbox"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestOutboxRepositoryContract(t *testing.T) {
	repositorytest.RunOutbox(t, func(t *testing.T) outbox.Repository {
		t.Helper()
		return memory.NewOutboxRepository()
	})
}
