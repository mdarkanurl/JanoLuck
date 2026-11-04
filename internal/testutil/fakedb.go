// Package testutil provides small helpers shared by package tests.
package testutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/database"
)

// DB describes how the fake database answers queries and executions.
type DB struct {
	Query func(query string, args []driver.NamedValue) (driver.Rows, error)
	Exec  func(query string, args []driver.NamedValue) (driver.Result, error)
}

// Rows is an in-memory result set returned by a fake query.
type Rows struct {
	Cols   []string
	Values [][]driver.Value
	pos    int
}

func (r *Rows) Columns() []string { return r.Cols }
func (r *Rows) Close() error      { return nil }

func (r *Rows) Next(dest []driver.Value) error {
	if r.pos >= len(r.Values) {
		return io.EOF
	}
	copy(dest, r.Values[r.pos])
	r.pos++
	return nil
}

// NewRows builds an in-memory result set.
func NewRows(columns []string, values ...[]driver.Value) *Rows {
	return &Rows{Cols: columns, Values: values}
}

// Result returns a successful driver result affecting n rows.
func Result(n int64) (driver.Result, error) {
	return driver.RowsAffected(n), nil
}

// QueryError returns a query handler result carrying err.
func QueryError(err error) (driver.Rows, error) {
	return nil, err
}

// ExecError returns an exec handler result carrying err.
func ExecError(err error) (driver.Result, error) {
	return nil, err
}

// NewQueries registers an isolated fake driver and returns database queries
// backed by it. The database is closed when the test finishes.
func NewQueries(t *testing.T, db *DB) *database.Queries {
	t.Helper()

	name := fmt.Sprintf("fakedb-%d", atomic.AddInt64(&driverCounter, 1))
	sql.Register(name, &driverFactory{db: db})

	raw, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open fake database: %v", err)
	}
	t.Cleanup(func() {
		_ = raw.Close()
	})

	return database.New(raw)
}

var driverCounter int64

type driverFactory struct {
	db *DB
}

func (f *driverFactory) Open(string) (driver.Conn, error) {
	return &conn{db: f.db}, nil
}

type conn struct {
	db *DB
}

func (c *conn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fake database: Prepare is not supported")
}

func (c *conn) Close() error { return nil }

func (c *conn) Begin() (driver.Tx, error) {
	return nil, errors.New("fake database: transactions are not supported")
}

func (c *conn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.db.Query == nil {
		return nil, fmt.Errorf("fake database: unexpected query: %s", query)
	}
	return c.db.Query(query, args)
}

func (c *conn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.db.Exec == nil {
		return nil, fmt.Errorf("fake database: unexpected exec: %s", query)
	}
	return c.db.Exec(query, args)
}
