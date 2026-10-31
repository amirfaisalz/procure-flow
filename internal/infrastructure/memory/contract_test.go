package memory_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
)

func TestProcurementRepositoryContract(t *testing.T) {
	repositorytest.Run(t, func(t *testing.T) procurement.Repository {
		return memory.NewProcurementRepository()
	})
}
