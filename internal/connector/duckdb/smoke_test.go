package duckdb

import (
	"context"
	"testing"
)

// TestConnectorSmoke confirms CGo build works and DuckDB driver is registered.
// Bypasses RecordIterator (added in slice 3) by going straight to sql.DB.
func TestConnectorSmoke(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	rows, err := c.DB().QueryContext(context.Background(), "SELECT 1, 'hello'")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("expected one row")
	}
	var n int
	var s string
	if err := rows.Scan(&n, &s); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1 || s != "hello" {
		t.Errorf("got (%d, %q), want (1, %q)", n, s, "hello")
	}
}

func TestConnectorDescribe(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	cols, err := c.Describe(context.Background(), "SELECT 1 AS a, 'x' AS b")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(cols) != 2 || cols[0].Name != "a" || cols[1].Name != "b" {
		t.Errorf("unexpected columns: %+v", cols)
	}
	// DuckDB returns INTEGER for unqualified `1`, VARCHAR for string literal.
	if cols[0].Type != "INTEGER" || cols[1].Type != "VARCHAR" {
		t.Errorf("unexpected types: %+v", cols)
	}
}
