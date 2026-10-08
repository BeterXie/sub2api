//go:build unit

package brand

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestScopeForBrandFallsBackFromDisabledPrimaryDomain(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT id,code,name,status").
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "code", "name", "status", "registration_enabled", "max_concurrent", "rpm_limit",
		}).AddRow(2, "mues", "MUES", "active", true, 10, 100))
	mock.ExpectQuery("SELECT id,brand_id,hostname,enabled,primary_flag").
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "brand_id", "hostname", "enabled", "primary_flag", "public_enabled", "gateway_enabled", "overrides", "metadata",
		}).
			AddRow(20, 2, "old.mues.cc", false, true, true, false, `{}`, `{}`).
			AddRow(21, 2, "api.mues.cc", true, false, true, true, `{}`, `{}`))

	scope, err := NewStore(db).ScopeForBrand(context.Background(), 2)

	require.NoError(t, err)
	require.Equal(t, int64(21), scope.DomainID)
	require.Equal(t, "api.mues.cc", scope.Hostname)
	require.Equal(t, "https://api.mues.cc", scope.CanonicalAPIOrigin)
	require.True(t, scope.GatewayEnabled)
	require.NoError(t, mock.ExpectationsWereMet())
}
