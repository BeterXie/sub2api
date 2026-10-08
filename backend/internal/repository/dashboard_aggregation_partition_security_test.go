//go:build unit

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCreateUsageLogsPartitionAppliesTenantSecurity(t *testing.T) {
	var executed string
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		executed = actual
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 0))

	repo := newDashboardAggregationRepositoryWithSQL(db)
	require.NoError(t, repo.createUsageLogsPartition(context.Background(), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, mock.ExpectationsWereMet())
	for _, fragment := range []string{
		`CREATE TABLE IF NOT EXISTS "usage_logs_202610" PARTITION OF usage_logs`,
		`ALTER TABLE "usage_logs_202610" ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE "usage_logs_202610" FORCE ROW LEVEL SECURITY`,
		`CREATE POLICY tenant_scope ON "usage_logs_202610"`,
		`CREATE TRIGGER enforce_brand`,
	} {
		require.True(t, strings.Contains(executed, fragment), fmt.Sprintf("partition SQL missing %q", fragment))
	}
}
