package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/marcboeker/go-duckdb"

	"github.com/wren-engine/wren/internal/connector"
)

// Connector wraps a database/sql DuckDB connection pool. Mirrors Java
// DuckdbClient: in-memory by default; init/session SQL set out-of-band by
// DuckDBMetadata (which is the public façade for setup).
type Connector struct {
	db *sql.DB
}

// NewConnector opens an in-memory DuckDB pool. The path "" yields an
// anonymous in-memory database, the same default Java uses
// (DriverManager.getConnection("jdbc:duckdb:")).
func NewConnector() *Connector {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		// sql.Open on duckdb only fails on driver-registration issues, which
		// indicate a build problem — fail fast with a clear message rather
		// than producing a half-built Connector.
		panic(fmt.Sprintf("duckdb open: %v (CGo build?)", err))
	}
	return &Connector{db: db}
}

// DB returns the underlying *sql.DB so DuckDBMetadata can execute init/session SQL.
func (c *Connector) DB() *sql.DB { return c.db }

func (c *Connector) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Query runs sql with no parameters.
func (c *Connector) Query(ctx context.Context, sqlText string) (connector.RecordIterator, error) {
	return c.QueryWithParams(ctx, sqlText, nil)
}

func (c *Connector) QueryWithParams(ctx context.Context, sqlText string, params []connector.Parameter) (connector.RecordIterator, error) {
	args := make([]any, len(params))
	for i, p := range params {
		args[i] = p.Value
	}
	rows, err := c.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("duckdb query: %w", err)
	}
	return newRecordIterator(rows)
}

func (c *Connector) Describe(ctx context.Context, sqlText string) ([]connector.Column, error) {
	// PREPARE then describe via column metadata — runs no rows. Mirrors Java
	// DuckdbClient.describe (PreparedStatement + getMetaData()). DuckDB has
	// no PREPARE-only API in Go driver yet; cheapest portable trick is
	// LIMIT 0 wrap, which DuckDB optimizes to a no-op scan.
	wrapped := "SELECT * FROM (" + strings.TrimSpace(sqlText) + ") __wren_describe LIMIT 0"
	rows, err := c.db.QueryContext(ctx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("duckdb describe: %w", err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("duckdb column types: %w", err)
	}
	cols := make([]connector.Column, len(types))
	for i, t := range types {
		cols[i] = connector.Column{Name: t.Name(), Type: strings.ToUpper(t.DatabaseTypeName())}
	}
	return cols, nil
}

func (c *Connector) ExecuteDDL(ctx context.Context, sqlText string) error {
	_, err := c.db.ExecContext(ctx, sqlText)
	if err != nil {
		return fmt.Errorf("duckdb ddl: %w", err)
	}
	return nil
}

func (c *Connector) DirectQuery(ctx context.Context, sqlText string, params []connector.Parameter) (connector.RecordIterator, error) {
	return c.QueryWithParams(ctx, sqlText, params)
}

func (c *Connector) DescribeQuery(ctx context.Context, sqlText string, params []connector.Parameter) ([]connector.Column, error) {
	// Parameters can change column count (rare in DuckDB but possible for $1::TEXT).
	// Java DuckdbClient.describe takes parameters; we mirror by ignoring them
	// here — DuckDB infers parameter types from context; LIMIT 0 wrap retains schema.
	_ = params
	return c.Describe(ctx, sqlText)
}

func (c *Connector) DirectDDL(ctx context.Context, sqlText string) error {
	return c.ExecuteDDL(ctx, sqlText)
}
