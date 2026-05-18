package postgres

import (
	"context"
	"fmt"

	"github.com/wren-engine/wren/internal/connector"
)

// Connector implements the connector.Client interface for PostgreSQL.
type Connector struct {
	// TODO: Add pgxpool
}

// NewConnector creates a new PostgreSQL connector.
func NewConnector() *Connector {
	return &Connector{}
}

func (c *Connector) Query(ctx context.Context, sql string) (connector.RecordIterator, error) {
	return c.QueryWithParams(ctx, sql, nil)
}

func (c *Connector) QueryWithParams(ctx context.Context, sql string, params []connector.Parameter) (connector.RecordIterator, error) {
	return nil, fmt.Errorf("PostgreSQL connector not yet implemented")
}

func (c *Connector) Describe(ctx context.Context, sql string) ([]connector.Column, error) {
	return nil, fmt.Errorf("PostgreSQL connector not yet implemented")
}

func (c *Connector) ExecuteDDL(ctx context.Context, sql string) error {
	return fmt.Errorf("PostgreSQL connector not yet implemented")
}

func (c *Connector) Close() error {
	return nil
}
