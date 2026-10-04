package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPluginConfigAndAccountScopeAreSavedAtomically(t *testing.T) {
	for _, tc := range []struct {
		name     string
		routing  *service.PluginAccountRouting
		expected any
	}{
		{"global", nil, nil},
		{"empty", &service.PluginAccountRouting{AccountIDs: []int64{}}, `{"account_ids":[]}`},
		{"selected", &service.PluginAccountRouting{AccountIDs: []int64{2, 4}}, `{"account_ids":[2,4]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectExec(`UPDATE sub2api_plugin_installations\s+SET config_encrypted = \$2, account_routing = \$3::jsonb, updated_at = NOW\(\)\s+WHERE id = \$1 AND binary_sha256 = \$4 AND config_encrypted = \$5`).
				WithArgs(int64(9), "new-encrypted-config", tc.expected, "binary", "old-encrypted-config").
				WillReturnResult(sqlmock.NewResult(0, 1))
			require.NoError(t, (&pluginRepository{db: db}).UpdateConfig(context.Background(), 9, "new-encrypted-config", tc.routing, "binary", "old-encrypted-config"))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPluginConfigSaveRejectsStaleConfig(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectExec(`UPDATE sub2api_plugin_installations`).WithArgs(int64(9), "new", nil, "binary", "stale").WillReturnResult(sqlmock.NewResult(0, 0))
	require.ErrorIs(t, (&pluginRepository{db: db}).UpdateConfig(context.Background(), 9, "new", nil, "binary", "stale"), service.ErrPluginStateChanged)
	require.NoError(t, mock.ExpectationsWereMet())
}
