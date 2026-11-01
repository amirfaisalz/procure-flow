package testsupport_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/idempotency"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
)

func TestIdempotencyRepositoryContract(t *testing.T) {
	repositorytest.RunIdempotency(t, func(t *testing.T) idempotency.Repository {
		return testsupport.NewIdempotencyRepository()
	})
}
