package memory_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestIdempotencyRepositoryContract(t *testing.T) {
	repositorytest.RunIdempotency(t, func(t *testing.T) idempotency.Repository {
		return memory.NewIdempotencyRepository()
	})
}
