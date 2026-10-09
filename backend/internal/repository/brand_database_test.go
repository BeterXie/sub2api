//go:build multibrand

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This suite deliberately requires a dedicated PostgreSQL database; it must
// never silently pass by skipping when a local Docker CLI is unavailable.
func TestMultiBrandDatabase(t *testing.T) {
	dsn := os.Getenv("MULTIBRAND_TEST_DSN")
	require.NotEmpty(t, dsn, "set MULTIBRAND_TEST_DSN to a local PostgreSQL test server")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	control, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer control.Close()
	name := fmt.Sprintf("multibrand_%d", time.Now().UnixNano())
	_, err = control.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)
	defer func() {
		_, _ = control.ExecContext(context.Background(), "DROP DATABASE "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
	}()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		require.NoError(t, err)
		u.Path = "/" + name
		dsn = u.String()
	} else {
		dsn += " dbname=" + name
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	// Verify the upgrade against real historical rows and registration policy.
	historical := fstest.MapFS{}
	files, err := fs.ReadDir(migrations.FS, ".")
	require.NoError(t, err)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") || file.Name() >= "269_" {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, file.Name())
		require.NoError(t, err)
		historical[file.Name()] = &fstest.MapFile{Data: data}
	}
	require.NoError(t, applyMigrationsFS(ctx, admin, historical))
	_, err = admin.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('registration_enabled','false') ON CONFLICT(key) DO UPDATE SET value='false'")
	require.NoError(t, err)
	var oldUserID int64
	err = admin.QueryRowContext(ctx, "INSERT INTO users(email,password_hash,role,balance,concurrency,status) VALUES('legacy@multibrand.test','hash','admin',17.25,3,'active') RETURNING id").Scan(&oldUserID)
	require.NoError(t, err)
	// Production already has the brand migrations before the upstream 2FA
	// migration arrives. Exercise that order as well as a fresh installation.
	deployed := fstest.MapFS{}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") || file.Name() == "269_openai_totp_rotation.sql" {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, file.Name())
		require.NoError(t, err)
		deployed[file.Name()] = &fstest.MapFile{Data: data}
	}
	require.NoError(t, applyMigrationsFS(ctx, admin, deployed))
	require.NoError(t, validateBrandDatabase(ctx, admin, true))
	require.NoError(t, applyMigrationsFS(ctx, admin, migrations.FS))
	require.NoError(t, validateBrandDatabase(ctx, admin, true))
	var oldBrand int64
	var oldBalance float64
	var registration bool
	require.NoError(t, admin.QueryRowContext(ctx, "SELECT brand_id,balance FROM users WHERE id=$1", oldUserID).Scan(&oldBrand, &oldBalance))
	require.Equal(t, brand.LegacyID, oldBrand)
	require.Equal(t, 17.25, oldBalance)
	require.NoError(t, admin.QueryRowContext(ctx, "SELECT registration_enabled FROM brands WHERE id=1").Scan(&registration))
	require.False(t, registration)
	store := brand.NewStore(admin)
	role, err := store.AdminRole(ctx, oldUserID, 1)
	require.NoError(t, err)
	require.Equal(t, "super_admin", role)

	connector, err := pq.NewConnector(dsn)
	require.NoError(t, err)
	db := sql.OpenDB(newBrandConnector(connector))
	defer db.Close()
	db.SetMaxOpenConns(1) // Reuse the same connection while switching brands.
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	scopes := make([]context.Context, 5)
	users := make([]*ent.User, 5)
	groups := make([]*ent.Group, 5)
	for i := range scopes {
		scopes[i] = brand.WithScope(ctx, brand.Scope{ID: int64(i + 1), Code: fmt.Sprint(i + 1)})
		users[i], err = client.User.Create().SetEmail("shared@multibrand.test").SetPasswordHash("hash").Save(scopes[i])
		require.NoError(t, err)
		require.Equal(t, int64(i+1), users[i].BrandID)
		groups[i], err = client.Group.Create().SetName("shared-group").Save(scopes[i])
		require.NoError(t, err)
		var currentRole string
		require.NoError(t, db.QueryRowContext(scopes[i], "SELECT current_user").Scan(&currentRole))
		require.Equal(t, brand.RuntimeRole, currentRole)
		var count int
		require.NoError(t, db.QueryRowContext(scopes[i], "SELECT COUNT(*) FROM users WHERE email='shared@multibrand.test'").Scan(&count))
		require.Equal(t, 1, count)
	}
	t.Run("ent and direct SQL hide foreign IDs", func(t *testing.T) {
		_, err := client.User.Get(scopes[1], users[0].ID)
		require.True(t, ent.IsNotFound(err), "%v", err)
		err = client.User.UpdateOneID(users[0].ID).SetBalance(100).Exec(scopes[1])
		require.Error(t, err)
		result, err := db.ExecContext(scopes[1], "UPDATE users SET balance=100 WHERE id=$1", users[0].ID)
		require.NoError(t, err)
		n, err := result.RowsAffected()
		require.NoError(t, err)
		require.Zero(t, n)
		stmt, err := db.PrepareContext(scopes[0], "SELECT email FROM users WHERE id=$1")
		require.NoError(t, err)
		defer stmt.Close()
		var email string
		require.ErrorIs(t, stmt.QueryRowContext(scopes[1], users[0].ID).Scan(&email), sql.ErrNoRows)
		require.NoError(t, stmt.QueryRowContext(scopes[0], users[0].ID).Scan(&email))
	})
	t.Run("transactions cannot change authority", func(t *testing.T) {
		tx, err := db.BeginTx(scopes[0], nil)
		require.NoError(t, err)
		defer tx.Rollback()
		_, err = tx.ExecContext(scopes[1], "SELECT 1")
		require.ErrorIs(t, err, brand.ErrScope)
		_, err = tx.ExecContext(brand.CredentialContext(scopes[0]), "SELECT 1")
		require.ErrorIs(t, err, brand.ErrScope)
		require.NoError(t, tx.Rollback())
	})
	t.Run("relations and upserts cannot cross brands", func(t *testing.T) {
		_, err := client.APIKey.Create().SetKey("multibrand-wrong-group").SetUserID(users[1].ID).SetGroupID(groups[0].ID).SetName("wrong").Save(scopes[1])
		require.Error(t, err)
		_, err = client.Group.UpdateOneID(groups[1].ID).SetFallbackGroupID(groups[0].ID).Save(scopes[1])
		require.Error(t, err)
		var updatedID int64
		err = db.QueryRowContext(scopes[1], `INSERT INTO users(email,password_hash,balance)
			VALUES($1,'hash',9.5) ON CONFLICT (brand_id,LOWER(TRIM(email))) WHERE deleted_at IS NULL
			DO UPDATE SET balance=EXCLUDED.balance RETURNING id`, users[1].Email).Scan(&updatedID)
		require.NoError(t, err)
		require.Equal(t, users[1].ID, updatedID)
		var balance float64
		require.NoError(t, db.QueryRowContext(scopes[0], "SELECT balance FROM users WHERE id=$1", users[0].ID).Scan(&balance))
		require.Zero(t, balance)
		// PostgreSQL also rejects an attempt to update a foreign primary key via upsert.
		_, err = db.ExecContext(scopes[1], `INSERT INTO users(id,email,password_hash) VALUES($1,'foreign@multibrand.test','hash')
			ON CONFLICT(id) DO UPDATE SET email=EXCLUDED.email`, users[0].ID)
		require.Error(t, err)
	})
	t.Run("channel monitor facts keep group-less brands separate", func(t *testing.T) {
		account, err := client.Account.Create().
			SetName("multibrand-monitor").
			SetPlatform("openai").
			SetType("apikey").
			Save(ctx)
		require.NoError(t, err)
		bucket := time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Minute)
		for i := 0; i < 2; i++ {
			key, err := client.APIKey.Create().
				SetUserID(users[i].ID).
				SetKey(fmt.Sprintf("sk-monitor-brand-%d", i+1)).
				SetName("monitor").
				Save(scopes[i])
			require.NoError(t, err)
			_, err = db.ExecContext(scopes[i], `INSERT INTO usage_logs (
				user_id,api_key_id,account_id,request_id,model,requested_model,
				input_tokens,output_tokens,actual_cost,first_token_ms,duration_ms,created_at
			) VALUES ($1,$2,$3,'shared-monitor-request','gpt-brand-isolation','gpt-brand-isolation',10,5,1,100,500,$4)`,
				users[i].ID, key.ID, account.ID, bucket.Add(10*time.Second))
			require.NoError(t, err)
			_, err = db.ExecContext(scopes[i], `INSERT INTO ops_error_logs (
				request_id,user_id,api_key_id,account_id,platform,model,requested_model,
				error_phase,error_type,error_owner,status_code,upstream_status_code,created_at
			) VALUES ('shared-monitor-error',$1,$2,$3,'openai','gpt-brand-isolation','gpt-brand-isolation',
				'upstream','upstream_error','provider',500,500,$4)`,
				users[i].ID, key.ID, account.ID, bucket.Add(20*time.Second))
			require.NoError(t, err)
		}

		monitor := NewChannelMonitorV2Repository(admin)
		require.NoError(t, monitor.RecomputeRange(ctx, bucket, bucket.Add(time.Minute)))
		for _, table := range []string{
			"channel_monitor_v2_metrics_1m",
			"channel_monitor_v2_user_metrics_1m",
			"channel_monitor_v2_error_metrics_1m",
			"channel_monitor_v2_latency_histograms_1m",
			"channel_monitor_v2_metrics_rollup",
			"channel_monitor_v2_user_metrics_rollup",
			"channel_monitor_v2_error_metrics_rollup",
			"channel_monitor_v2_latency_histograms_rollup",
		} {
			var brands, rows int
			err := admin.QueryRowContext(ctx, `SELECT COUNT(DISTINCT brand_id),COUNT(*) FROM `+table+` WHERE model='gpt-brand-isolation'`).Scan(&brands, &rows)
			require.NoError(t, err, table)
			require.Equal(t, 2, brands, table)
			require.GreaterOrEqual(t, rows, 2, table)
		}
		for _, table := range []string{"channel_monitor_v2_metrics_1m", "channel_monitor_v2_metrics_rollup"} {
			where := ""
			if strings.HasSuffix(table, "_rollup") {
				where = " AND bucket_seconds=300"
			}
			rows, err := admin.QueryContext(ctx, `SELECT brand_id,SUM(success_requests),SUM(error_requests) FROM `+table+` WHERE model='gpt-brand-isolation'`+where+` GROUP BY brand_id ORDER BY brand_id`)
			require.NoError(t, err)
			defer rows.Close()
			for brandID := int64(1); brandID <= 2; brandID++ {
				require.True(t, rows.Next(), table)
				var gotBrand, successes, errors int64
				require.NoError(t, rows.Scan(&gotBrand, &successes, &errors))
				require.Equal(t, brandID, gotBrand)
				require.Equal(t, int64(1), successes)
				require.Equal(t, int64(1), errors)
			}
			require.False(t, rows.Next(), table)
			require.NoError(t, rows.Err())
		}
	})
	t.Run("ops outcomes without keys stay separate across brands", func(t *testing.T) {
		start := time.Date(2005, 1, 1, 0, 0, 0, 0, time.UTC)
		end := start.Add(time.Hour)
		for i := 0; i < 2; i++ {
			for duplicate := 0; duplicate < 2; duplicate++ {
				_, err := db.ExecContext(scopes[i], `INSERT INTO ops_error_logs (
					request_id,platform,error_phase,error_type,error_owner,status_code,created_at
				) VALUES ('same-unattributed-request','openai','upstream','upstream_error','provider',502,$1)`,
					start.Add(time.Duration(duplicate)*time.Minute))
				require.NoError(t, err)
			}
			ops := NewOpsRepository(db).(*opsRepository)
			total, _, sla, _, _, _, err := ops.queryErrorCounts(scopes[i], nil, start, end)
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			require.Equal(t, total, sla)
		}
		ops := NewOpsRepository(admin).(*opsRepository)
		total, _, sla, _, _, _, err := ops.queryErrorCounts(ctx, nil, start, end)
		require.NoError(t, err)
		require.EqualValues(t, 2, total, "deduplicate within each brand, including requests without API keys or groups")
		require.Equal(t, total, sla)
	})
	t.Run("aliases share LLMP and forged headers cannot choose a brand", func(t *testing.T) {
		_, err := admin.ExecContext(ctx, "UPDATE domains SET enabled=TRUE WHERE brand_id=1")
		require.NoError(t, err)
		for _, host := range []string{"llmp.org", "LLMP.CC:443", "llmp.xyz.", "llmp.site"} {
			scope, err := store.Resolve(ctx, host)
			require.NoError(t, err)
			require.Equal(t, int64(1), scope.ID)
		}
		_, err = store.Resolve(ctx, "unknown.test")
		require.ErrorIs(t, err, brand.ErrUnknownDomain)
	})
	t.Run("disabling isolation with independent data fails closed", func(t *testing.T) {
		require.Error(t, validateBrandDatabase(ctx, admin, false))
	})
}
