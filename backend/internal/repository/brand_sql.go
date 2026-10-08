package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/brand"
)

// brandConnector sets the role and tenant on the connection executing the
// statement. A scoped transaction pins its authority until commit/rollback.
type brandConnector struct{ base driver.Connector }

func newBrandConnector(base driver.Connector) driver.Connector { return &brandConnector{base} }
func (c *brandConnector) Driver() driver.Driver                { return c.base.Driver() }
func (c *brandConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &brandConn{Conn: conn}, nil
}

type brandConn struct {
	driver.Conn
	inTx             bool
	txScope          brand.Scope
	txScoped         bool
	scopeInitialized bool
	currentBrand     int64
}

func (c *brandConn) applyScope(ctx context.Context) error {
	scope, scoped := brand.FromContext(ctx)
	if c.inTx {
		if scoped && (!c.txScoped || scope.ID != c.txScope.ID || scope.Platform != c.txScope.Platform) {
			return brand.ErrScope
		}
		return nil
	}
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return fmt.Errorf("PostgreSQL driver lacks contextual execution")
	}
	roleSQL := "RESET ROLE"
	id := ""
	var authorityID int64
	if scoped && !scope.Platform {
		roleSQL = "SET ROLE sub2api_brand_runtime"
		id = strconv.FormatInt(scope.ID, 10)
		authorityID = scope.ID
	}
	if c.scopeInitialized && c.currentBrand == authorityID {
		return nil
	}
	c.scopeInitialized = false
	if _, err := execer.ExecContext(ctx, roleSQL, nil); err != nil {
		return err
	}
	_, err := execer.ExecContext(ctx, "SELECT set_config('sub2api.brand_id', $1, false)", []driver.NamedValue{{Ordinal: 1, Value: id}})
	if err == nil {
		c.scopeInitialized = true
		c.currentBrand = authorityID
	}
	return err
}
func (c *brandConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.applyScope(ctx); err != nil {
		return nil, err
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}
func (c *brandConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.applyScope(ctx); err != nil {
		return nil, err
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}
func (c *brandConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *brandConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.applyScope(ctx); err != nil {
		return nil, err
	}
	stmt, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &brandStmt{Stmt: stmt, conn: c}, nil
}
func (c *brandConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *brandConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := c.applyScope(ctx); err != nil {
		return nil, err
	}
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	c.txScope, c.txScoped = brand.FromContext(ctx)
	c.inTx = true
	return &brandTx{Tx: tx, conn: c}, nil
}
func (c *brandConn) ResetSession(ctx context.Context) error {
	if c.inTx {
		return driver.ErrBadConn
	}
	c.scopeInitialized = false
	if reset, ok := c.Conn.(driver.SessionResetter); ok {
		if err := reset.ResetSession(ctx); err != nil {
			return err
		}
	}
	return nil
}
func (c *brandConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}
func (c *brandConn) Ping(ctx context.Context) error {
	if err := c.applyScope(ctx); err != nil {
		return err
	}
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}
func (c *brandConn) CheckNamedValue(v *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(v)
	}
	return driver.ErrSkip
}

type brandStmt struct {
	driver.Stmt
	conn *brandConn
}

func (s *brandStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.conn.applyScope(ctx); err != nil {
		return nil, err
	}
	if stmt, ok := s.Stmt.(driver.StmtExecContext); ok {
		return stmt.ExecContext(ctx, args)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := brandStmtValues(args)
	if err != nil {
		return nil, err
	}
	return s.Stmt.Exec(values)
}
func (s *brandStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.conn.applyScope(ctx); err != nil {
		return nil, err
	}
	if stmt, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return stmt.QueryContext(ctx, args)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := brandStmtValues(args)
	if err != nil {
		return nil, err
	}
	return s.Stmt.Query(values)
}
func brandStmtValues(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			return nil, fmt.Errorf("PostgreSQL statement does not support named parameters")
		}
		values[i] = arg.Value
	}
	return values, nil
}
func (s *brandStmt) Exec(args []driver.Value) (driver.Result, error) {
	values := make([]driver.NamedValue, len(args))
	for i, v := range args {
		values[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.ExecContext(context.Background(), values)
}
func (s *brandStmt) Query(args []driver.Value) (driver.Rows, error) {
	values := make([]driver.NamedValue, len(args))
	for i, v := range args {
		values[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.QueryContext(context.Background(), values)
}

type brandTx struct {
	driver.Tx
	conn *brandConn
}

func (t *brandTx) Commit() error {
	err := t.Tx.Commit()
	t.conn.inTx = false
	t.conn.scopeInitialized = false
	return err
}
func (t *brandTx) Rollback() error {
	err := t.Tx.Rollback()
	t.conn.inTx = false
	t.conn.scopeInitialized = false
	return err
}
