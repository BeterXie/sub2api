package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/lib/pq"
)

// Turning off ingress isolation is safe only before independent tenants exist.
// Runtime policy enforcement must work even with a privileged migration login.
func validateBrandDatabase(ctx context.Context, db *sql.DB, enabled bool) error {
	if !enabled {
		populated, err := hasIndependentBrandData(ctx, db)
		if err != nil {
			return err
		}
		if !populated {
			err = db.QueryRowContext(ctx, `SELECT EXISTS(
				SELECT 1 FROM brands WHERE id<>$1 AND status='active')`, brand.LegacyID).Scan(&populated)
			if err != nil {
				return err
			}
		}
		if populated {
			return fmt.Errorf("multibrand must remain enabled after independent brands are activated; disable individual brands/domains instead")
		}
		return nil
	}
	var unsafe, member bool
	err := db.QueryRowContext(ctx, `SELECT
		rolsuper OR rolbypassrls OR rolcanlogin OR EXISTS(
			SELECT 1 FROM pg_class WHERE relowner=pg_roles.oid AND relnamespace='public'::regnamespace),
		pg_has_role(current_user,oid,'MEMBER')
		FROM pg_roles WHERE rolname=$1`, brand.RuntimeRole).Scan(&unsafe, &member)
	if err != nil {
		return fmt.Errorf("verify multibrand runtime role: %w", err)
	}
	if unsafe || !member {
		return fmt.Errorf("multibrand requires the non-login, non-owner, NOSUPERUSER NOBYPASSRLS role %s granted to the database login", brand.RuntimeRole)
	}
	var missingIsolation bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='brand_id' AND NOT a.attisdropped
		WHERE n.nspname='public' AND c.relkind='r' AND c.relname NOT IN ('domains','brand_admins')
		AND (NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR
			NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='tenant_scope') OR
			NOT EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid=c.oid AND t.tgname='enforce_brand' AND t.tgenabled='O'))
	)`).Scan(&missingIsolation)
	if err != nil {
		return fmt.Errorf("verify multibrand row policies: %w", err)
	}
	if missingIsolation {
		return fmt.Errorf("multibrand row policies or ownership triggers are missing or disabled")
	}
	var unsafeGrants bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM unnest(ARRAY['brands','domains','brand_admins','security_secrets','settings']) t
		WHERE has_table_privilege($1,t,'INSERT,UPDATE,DELETE,TRUNCATE,TRIGGER,REFERENCES')
	)`, brand.RuntimeRole).Scan(&unsafeGrants)
	if err != nil {
		return err
	}
	if unsafeGrants {
		return fmt.Errorf("multibrand runtime role must not mutate platform settings or operator grants")
	}
	return nil
}

func hasIndependentBrandData(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT n.nspname,c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='brand_id' AND NOT a.attisdropped
		WHERE n.nspname='public' AND c.relkind IN ('r','p')
		  AND c.relname NOT IN ('domains','brand_admins')
		  AND NOT EXISTS(SELECT 1 FROM pg_inherits i WHERE i.inhrelid=c.oid)
		ORDER BY c.relname`)
	if err != nil {
		return false, err
	}
	var tables [][2]string
	for rows.Next() {
		var schemaName, tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			_ = rows.Close()
			return false, err
		}
		tables = append(tables, [2]string{schemaName, tableName})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}

	for _, table := range tables {
		qualified := pq.QuoteIdentifier(table[0]) + "." + pq.QuoteIdentifier(table[1])
		var populated bool
		query := "SELECT EXISTS(SELECT 1 FROM " + qualified + " WHERE brand_id<>$1)"
		if err := db.QueryRowContext(ctx, query, brand.LegacyID).Scan(&populated); err != nil {
			return false, fmt.Errorf("inspect %s brand ownership: %w", qualified, err)
		}
		if populated {
			return true, nil
		}
	}
	return false, nil
}
