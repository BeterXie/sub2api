//go:build multibrand

package repository

import (
	"database/sql"
	"github.com/lib/pq"
)

// NewBrandTestDatabase lets external HTTP tests use the exact production
// connector without adding a test entry point to the deployed application.
func NewBrandTestDatabase(dsn string) (*sql.DB, error) {
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(newBrandConnector(connector)), nil
}
