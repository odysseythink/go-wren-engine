package duckdb

import (
	"context"
	"fmt"

	"github.com/wren-engine/wren/internal/connector"
)

// Connector implements the connector.Client interface for DuckDB.
type Connector struct {
	// TODO: Add sql.DB pool
}

// NewConnector creates a new DuckDB connector.
func NewConnector() *Connector {
	return &Connector{}
}

func (c *Connector) Query(ctx context.Context, sql string) (connector.RecordIterator, error) {
	return c.QueryWithParams(ctx, sql, nil)
}

func (c *Connector) QueryWithParams(ctx context.Context, sql string, params []connector.Parameter) (connector.RecordIterator, error) {
	return nil, fmt.Errorf("DuckDB connector not yet implemented")
}

func (c *Connector) Describe(ctx context.Context, sql string) ([]connector.Column, error) {
	return nil, fmt.Errorf("DuckDB connector not yet implemented")
}

func (c *Connector) ExecuteDDL(ctx context.Context, sql string) error {
	return fmt.Errorf("DuckDB connector not yet implemented")
}

func (c *Connector) Close() error {
	return nil
}

func (c *Connector) DirectQuery(ctx context.Context, sql string, params []connector.Parameter) (connector.RecordIterator, error) {
	return c.QueryWithParams(ctx, sql, params)
}

func (c *Connector) DescribeQuery(ctx context.Context, sql string, params []connector.Parameter) ([]connector.Column, error) {
	return c.Describe(ctx, sql)
}

func (c *Connector) DirectDDL(ctx context.Context, sql string) error {
	return c.ExecuteDDL(ctx, sql)
}
