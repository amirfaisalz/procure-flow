package testsupport_test

import (
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/repositorytest"
	"github.com/amirfaisalz/procure-flow/internal/testsupport"
)

func TestAuditRepositoryContract(t *testing.T) {
	repositorytest.RunAudit(t, func(t *testing.T) audit.Repository {
		return testsupport.NewAuditRepository()
	})
}
