package repository

import (
	"context"
	"database/sql/driver"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

const fakeDriverDelay = 2 * time.Millisecond

type timingFakeDriver struct{}

func (timingFakeDriver) Open(string) (driver.Conn, error) { return newTimingFakeConn(), nil }

type timingFakeConnector struct{ conn driver.Conn }

func (c timingFakeConnector) Connect(context.Context) (driver.Conn, error) {
	time.Sleep(fakeDriverDelay)
	return c.conn, nil
}

func (c timingFakeConnector) Driver() driver.Driver { return timingFakeDriver{} }

type timingFakeConn struct{}

func newTimingFakeConn() *timingFakeConn { return &timingFakeConn{} }

func (c *timingFakeConn) Prepare(string) (driver.Stmt, error) {
	time.Sleep(fakeDriverDelay)
	return &timingFakeStmt{}, nil
}

func (c *timingFakeConn) PrepareContext(context.Context, string) (driver.Stmt, error) {
	time.Sleep(fakeDriverDelay)
	return &timingFakeStmt{}, nil
}

func (c *timingFakeConn) Close() error { return nil }
func (c *timingFakeConn) Begin() (driver.Tx, error) {
	time.Sleep(fakeDriverDelay)
	return &timingFakeTx{}, nil
}
func (c *timingFakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	time.Sleep(fakeDriverDelay)
	return &timingFakeTx{}, nil
}
func (c *timingFakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	time.Sleep(fakeDriverDelay)
	return driver.RowsAffected(1), nil
}
func (c *timingFakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	time.Sleep(fakeDriverDelay)
	return &timingFakeRows{values: [][]driver.Value{{"value"}}}, nil
}
func (c *timingFakeConn) Ping(context.Context) error {
	time.Sleep(fakeDriverDelay)
	return nil
}
func (c *timingFakeConn) ResetSession(context.Context) error {
	time.Sleep(fakeDriverDelay)
	return nil
}

type timingFakeStmt struct{ closed bool }

func (s *timingFakeStmt) Close() error  { s.closed = true; return nil }
func (s *timingFakeStmt) NumInput() int { return -1 }
func (s *timingFakeStmt) Exec([]driver.Value) (driver.Result, error) {
	time.Sleep(fakeDriverDelay)
	return driver.RowsAffected(1), nil
}
func (s *timingFakeStmt) Query([]driver.Value) (driver.Rows, error) {
	time.Sleep(fakeDriverDelay)
	return &timingFakeRows{values: [][]driver.Value{{"value"}}}, nil
}
func (s *timingFakeStmt) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	time.Sleep(fakeDriverDelay)
	return driver.RowsAffected(1), nil
}
func (s *timingFakeStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	time.Sleep(fakeDriverDelay)
	return &timingFakeRows{values: [][]driver.Value{{"value"}}}, nil
}

type timingFakeRows struct {
	values [][]driver.Value
	index  int
}

func (r *timingFakeRows) Columns() []string { return []string{"value"} }
func (r *timingFakeRows) Close() error {
	time.Sleep(fakeDriverDelay)
	return nil
}
func (r *timingFakeRows) Next(dest []driver.Value) error {
	time.Sleep(fakeDriverDelay)
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

type timingFakeTx struct{ rolledBack bool }

func (t *timingFakeTx) Commit() error {
	time.Sleep(fakeDriverDelay)
	return nil
}
func (t *timingFakeTx) Rollback() error {
	t.rolledBack = true
	time.Sleep(fakeDriverDelay)
	return nil
}

func timingMetricDuration(t *testing.T, header, metric string) float64 {
	t.Helper()
	re := regexp.MustCompile(`(?:^|, )` + regexp.QuoteMeta(metric) + `;dur=([0-9]+(?:\.[0-9]+)?)`)
	match := re.FindStringSubmatch(header)
	if len(match) != 2 {
		t.Fatalf("metric %q missing from header %q", metric, header)
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		t.Fatalf("parse %s duration: %v", metric, err)
	}
	return value
}

func TestServerTimingConnectorRecordsDriverBlockingWithoutRowLifetime(t *testing.T) {
	collector := servertiming.New(time.Now())
	ctx := servertiming.WithCollector(context.Background(), collector)
	rawConn, err := newServerTimingConnector(timingFakeConnector{conn: newTimingFakeConn()}).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, ok := rawConn.(*serverTimingConn)
	if !ok {
		t.Fatalf("connection type = %T, want *serverTimingConn", rawConn)
	}

	if _, err := conn.ExecContext(ctx, "sensitive update", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.QueryContext(ctx, "sensitive select", nil)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]driver.Value, 1)
	if err := rows.Next(values); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := rows.Next(values); err != io.EOF {
		t.Fatalf("rows.Next() = %v, want EOF", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	header := collector.HeaderValue(time.Now(), "bypass")
	if !strings.Contains(header, `queries=2`) || strings.Contains(header, "sensitive") {
		t.Fatalf("unexpected SQL header %q", header)
	}
	if app, db := timingMetricDuration(t, header, "app"), timingMetricDuration(t, header, "db"); app <= db {
		t.Fatalf("row processing gap counted as DB time: app=%.1f db=%.1f header=%q", app, db, header)
	}
}

func TestServerTimingPreparedStatementsTransactionsAndOptionalInterfaces(t *testing.T) {
	collector := servertiming.New(time.Now())
	ctx := servertiming.WithCollector(context.Background(), collector)
	conn := &serverTimingConn{Conn: newTimingFakeConn()}

	stmt, err := conn.PrepareContext(ctx, "prepare sensitive statement")
	if err != nil {
		t.Fatal(err)
	}
	timedStmt, ok := stmt.(*serverTimingStmt)
	if !ok {
		t.Fatalf("statement type = %T, want *serverTimingStmt", stmt)
	}
	if _, err := timedStmt.ExecContext(ctx, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := timedStmt.QueryContext(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if timedStmt.ColumnConverter(0) == nil {
		t.Fatal("legacy ColumnConverter fallback was not preserved")
	}

	tx, err := conn.BeginTx(ctx, driver.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := conn.ResetSession(ctx); err != nil {
		t.Fatal(err)
	}

	header := collector.HeaderValue(time.Now(), "bypass")
	if !strings.Contains(header, `queries=3`) || timingMetricDuration(t, header, "db") <= 0 {
		t.Fatalf("driver metrics missing: %q", header)
	}
}

type legacyTimingConn struct {
	stmt *timingFakeStmt
	tx   *timingFakeTx
}

func (c *legacyTimingConn) Prepare(string) (driver.Stmt, error) {
	c.stmt = &timingFakeStmt{}
	return c.stmt, nil
}
func (c *legacyTimingConn) Close() error { return nil }
func (c *legacyTimingConn) Begin() (driver.Tx, error) {
	c.tx = &timingFakeTx{}
	return c.tx, nil
}
func (c *legacyTimingConn) Exec(string, []driver.Value) (driver.Result, error) { //nolint:staticcheck
	return driver.RowsAffected(1), nil
}
func (c *legacyTimingConn) Query(string, []driver.Value) (driver.Rows, error) { //nolint:staticcheck
	return &timingFakeRows{}, nil
}

func TestServerTimingLegacyDriverHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	legacy := &legacyTimingConn{}
	conn := &serverTimingConn{Conn: legacy}

	if _, err := conn.ExecContext(ctx, "query", nil); err != context.Canceled {
		t.Fatalf("ExecContext() error = %v, want context.Canceled", err)
	}
	if _, err := conn.QueryContext(ctx, "query", nil); err != context.Canceled {
		t.Fatalf("QueryContext() error = %v, want context.Canceled", err)
	}
	if _, err := conn.PrepareContext(ctx, "query"); err != context.Canceled {
		t.Fatalf("PrepareContext() error = %v, want context.Canceled", err)
	}
	if legacy.stmt == nil || !legacy.stmt.closed {
		t.Fatal("prepared statement was not closed after cancellation")
	}
	if _, err := conn.BeginTx(ctx, driver.TxOptions{}); err != context.Canceled {
		t.Fatalf("BeginTx() error = %v, want context.Canceled", err)
	}
	if legacy.tx == nil || !legacy.tx.rolledBack {
		t.Fatal("transaction was not rolled back after cancellation")
	}
}

func TestNamedValuesRejectNamedParameters(t *testing.T) {
	if _, err := namedValues([]driver.NamedValue{{Name: "secret", Value: 1}}); err == nil {
		t.Fatal("namedValues accepted a named parameter")
	}
	values, err := namedValues([]driver.NamedValue{{Ordinal: 1, Value: "value"}})
	if err != nil || len(values) != 1 || values[0] != "value" {
		t.Fatalf("namedValues() = %#v, %v", values, err)
	}
}
