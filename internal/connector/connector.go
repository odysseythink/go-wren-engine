package connector

import "context"

// Column describes a column in a query result.
type Column struct {
	Name string
	Type string
}

// Parameter represents a query parameter.
type Parameter struct {
	Name  string
	Value any
}

// RecordIterator iterates over query results.
type RecordIterator interface {
	Next() bool
	Get() []any
	Columns() []Column
	Close() error
}

// Client is the interface for data source connectors.
type Client interface {
	Query(ctx context.Context, sql string) (RecordIterator, error)
	QueryWithParams(ctx context.Context, sql string, params []Parameter) (RecordIterator, error)
	Describe(ctx context.Context, sql string) ([]Column, error)
	ExecuteDDL(ctx context.Context, sql string) error
	Close() error
}

// Metadata provides query execution on the underlying data source.
type Metadata interface {
	DirectQuery(ctx context.Context, sql string, params []Parameter) (RecordIterator, error)
	DescribeQuery(ctx context.Context, sql string, params []Parameter) ([]Column, error)
	DirectDDL(ctx context.Context, sql string) error
}
