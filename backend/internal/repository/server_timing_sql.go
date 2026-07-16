package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

type serverTimingConnector struct {
	base driver.Connector
}

func newServerTimingConnector(base driver.Connector) driver.Connector {
	if base == nil {
		panic("server timing SQL connector must not be nil")
	}
	return &serverTimingConnector{base: base}
}

func (c *serverTimingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	startedAt := time.Now()
	conn, err := c.base.Connect(ctx)
	servertiming.RecordInterval(ctx, servertiming.MetricDatabase, startedAt, time.Now())
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("SQL connector returned a nil connection without an error")
	}
	return &serverTimingConn{Conn: conn}, nil
}

func (c *serverTimingConnector) Driver() driver.Driver {
	return c.base.Driver()
}

type serverTimingConn struct {
	driver.Conn
}

func (c *serverTimingConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	if stmt == nil {
		return nil, errors.New("SQL driver returned a nil statement without an error")
	}
	return &serverTimingStmt{Stmt: stmt}, nil
}

func (c *serverTimingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	startedAt := time.Now()
	var (
		stmt driver.Stmt
		err  error
	)
	if preparer, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err = preparer.PrepareContext(ctx, query)
	} else {
		stmt, err = c.Conn.Prepare(query)
		if err == nil {
			select {
			default:
			case <-ctx.Done():
				if stmt != nil {
					_ = stmt.Close()
				}
				err = ctx.Err()
			}
		}
	}
	servertiming.Record(ctx, servertiming.MetricDatabase, startedAt, time.Now(), 1)
	if err != nil {
		return nil, err
	}
	if stmt == nil {
		return nil, errors.New("SQL driver returned a nil statement without an error")
	}
	return &serverTimingStmt{Stmt: stmt}, nil
}

func (c *serverTimingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	startedAt := time.Now()
	var (
		result driver.Result
		err    error
	)
	if execer, ok := c.Conn.(driver.ExecerContext); ok {
		result, err = execer.ExecContext(ctx, query, args)
	} else if execer, ok := c.Conn.(driver.Execer); ok { //nolint:staticcheck // Preserve legacy driver semantics.
		var values []driver.Value
		values, err = namedValues(args)
		if err == nil {
			select {
			case <-ctx.Done():
				err = ctx.Err()
			default:
				result, err = execer.Exec(query, values) //nolint:staticcheck // Preserve legacy driver semantics.
			}
		}
	} else {
		return nil, driver.ErrSkip
	}
	servertiming.Record(ctx, servertiming.MetricDatabase, startedAt, time.Now(), 1)
	return result, err
}

func (c *serverTimingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	startedAt := time.Now()
	var (
		rows driver.Rows
		err  error
	)
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		rows, err = queryer.QueryContext(ctx, query, args)
	} else if queryer, ok := c.Conn.(driver.Queryer); ok { //nolint:staticcheck // Preserve legacy driver semantics.
		var values []driver.Value
		values, err = namedValues(args)
		if err == nil {
			select {
			case <-ctx.Done():
				err = ctx.Err()
			default:
				rows, err = queryer.Query(query, values) //nolint:staticcheck // Preserve legacy driver semantics.
			}
		}
	} else {
		return nil, driver.ErrSkip
	}
	servertiming.Record(ctx, servertiming.MetricDatabase, startedAt, time.Now(), 1)
	if err != nil {
		return rows, err
	}
	if rows == nil {
		return nil, errors.New("SQL driver returned nil rows without an error")
	}
	return newServerTimingRows(ctx, rows), nil
}

func (c *serverTimingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	startedAt := time.Now()
	var (
		tx  driver.Tx
		err error
	)
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		tx, err = beginner.BeginTx(ctx, opts)
	} else {
		if opts.Isolation != driver.IsolationLevel(0) {
			return nil, errors.New("sql: driver does not support non-default isolation level")
		}
		if opts.ReadOnly {
			return nil, errors.New("sql: driver does not support read-only transactions")
		}
		tx, err = c.Conn.Begin() //nolint:staticcheck // Required database/sql compatibility fallback.
		if err == nil && ctx.Done() != nil {
			select {
			default:
			case <-ctx.Done():
				if tx != nil {
					_ = tx.Rollback()
				}
				err = ctx.Err()
			}
		}
	}
	servertiming.RecordInterval(ctx, servertiming.MetricDatabase, startedAt, time.Now())
	if err != nil {
		return tx, err
	}
	if tx == nil {
		return nil, errors.New("SQL driver returned a nil transaction without an error")
	}
	return &serverTimingTx{Tx: tx, ctx: ctx}, nil
}

func (c *serverTimingConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		startedAt := time.Now()
		err := pinger.Ping(ctx)
		servertiming.RecordInterval(ctx, servertiming.MetricDatabase, startedAt, time.Now())
		return err
	}
	return nil
}

func (c *serverTimingConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		startedAt := time.Now()
		err := resetter.ResetSession(ctx)
		servertiming.RecordInterval(ctx, servertiming.MetricDatabase, startedAt, time.Now())
		return err
	}
	return nil
}

func (c *serverTimingConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

func (c *serverTimingConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

type serverTimingStmt struct {
	driver.Stmt
}

func (s *serverTimingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	startedAt := time.Now()
	var (
		result driver.Result
		err    error
	)
	if execer, ok := s.Stmt.(driver.StmtExecContext); ok {
		result, err = execer.ExecContext(ctx, args)
	} else {
		var values []driver.Value
		values, err = namedValues(args)
		if err == nil {
			select {
			case <-ctx.Done():
				err = ctx.Err()
			default:
				result, err = s.Stmt.Exec(values) //nolint:staticcheck // Required database/sql compatibility fallback.
			}
		}
	}
	servertiming.Record(ctx, servertiming.MetricDatabase, startedAt, time.Now(), 1)
	return result, err
}

func (s *serverTimingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	startedAt := time.Now()
	var (
		rows driver.Rows
		err  error
	)
	if queryer, ok := s.Stmt.(driver.StmtQueryContext); ok {
		rows, err = queryer.QueryContext(ctx, args)
	} else {
		var values []driver.Value
		values, err = namedValues(args)
		if err == nil {
			select {
			case <-ctx.Done():
				err = ctx.Err()
			default:
				rows, err = s.Stmt.Query(values) //nolint:staticcheck // Required database/sql compatibility fallback.
			}
		}
	}
	servertiming.Record(ctx, servertiming.MetricDatabase, startedAt, time.Now(), 1)
	if err != nil {
		return rows, err
	}
	if rows == nil {
		return nil, errors.New("SQL driver returned nil rows without an error")
	}
	return newServerTimingRows(ctx, rows), nil
}

func (s *serverTimingStmt) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := s.Stmt.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

func (s *serverTimingStmt) ColumnConverter(index int) driver.ValueConverter {
	if converter, ok := s.Stmt.(driver.ColumnConverter); ok { //nolint:staticcheck // Preserve legacy driver interface.
		return converter.ColumnConverter(index) //nolint:staticcheck // Preserve legacy driver interface.
	}
	return driver.DefaultParameterConverter
}

func namedValues(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			return nil, errors.New("sql: driver does not support the use of Named Parameters")
		}
		values[i] = arg.Value
	}
	return values, nil
}

type serverTimingRows struct {
	driver.Rows
	ctx context.Context
}

func newServerTimingRows(ctx context.Context, rows driver.Rows) *serverTimingRows {
	return &serverTimingRows{Rows: rows, ctx: ctx}
}

func (r *serverTimingRows) Close() error {
	startedAt := time.Now()
	err := r.Rows.Close()
	servertiming.RecordInterval(r.ctx, servertiming.MetricDatabase, startedAt, time.Now())
	return err
}

func (r *serverTimingRows) Next(dest []driver.Value) error {
	startedAt := time.Now()
	err := r.Rows.Next(dest)
	servertiming.RecordInterval(r.ctx, servertiming.MetricDatabase, startedAt, time.Now())
	return err
}

func (r *serverTimingRows) HasNextResultSet() bool {
	if rows, ok := r.Rows.(driver.RowsNextResultSet); ok {
		return rows.HasNextResultSet()
	}
	return false
}

func (r *serverTimingRows) NextResultSet() error {
	rows, ok := r.Rows.(driver.RowsNextResultSet)
	if !ok {
		return io.EOF
	}
	startedAt := time.Now()
	err := rows.NextResultSet()
	servertiming.RecordInterval(r.ctx, servertiming.MetricDatabase, startedAt, time.Now())
	return err
}

func (r *serverTimingRows) ColumnTypeScanType(index int) reflect.Type {
	if rows, ok := r.Rows.(driver.RowsColumnTypeScanType); ok {
		return rows.ColumnTypeScanType(index)
	}
	return reflect.TypeFor[any]()
}

func (r *serverTimingRows) ColumnTypeDatabaseTypeName(index int) string {
	if rows, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return rows.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (r *serverTimingRows) ColumnTypeLength(index int) (int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeLength); ok {
		return rows.ColumnTypeLength(index)
	}
	return 0, false
}

func (r *serverTimingRows) ColumnTypeNullable(index int) (bool, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeNullable); ok {
		return rows.ColumnTypeNullable(index)
	}
	return false, false
}

func (r *serverTimingRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return rows.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

type serverTimingTx struct {
	driver.Tx
	ctx context.Context
}

func (t *serverTimingTx) Commit() error {
	startedAt := time.Now()
	err := t.Tx.Commit()
	servertiming.RecordInterval(t.ctx, servertiming.MetricDatabase, startedAt, time.Now())
	return err
}

func (t *serverTimingTx) Rollback() error {
	startedAt := time.Now()
	err := t.Tx.Rollback()
	servertiming.RecordInterval(t.ctx, servertiming.MetricDatabase, startedAt, time.Now())
	return err
}
