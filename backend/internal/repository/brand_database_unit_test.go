//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestHasIndependentBrandDataChecksEveryTenantTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT n.nspname,c.relname").WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname"}).
			AddRow("public", "announcements").
			AddRow("public", "usage_logs"),
	)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "public"\."announcements" WHERE brand_id<>\$1\)`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM "public"\."usage_logs" WHERE brand_id<>\$1\)`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	populated, err := hasIndependentBrandData(context.Background(), db)
	require.NoError(t, err)
	require.True(t, populated)
	require.NoError(t, mock.ExpectationsWereMet())
}
