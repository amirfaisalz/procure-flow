package testsupport_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
)

func TestProcurementRepositoryContract(t *testing.T) {
	repositorytest.Run(t, func(t *testing.T) procurement.Repository {
		return testsupport.NewProcurementRepository()
	})
}
