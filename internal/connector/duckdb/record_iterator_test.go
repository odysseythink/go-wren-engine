package duckdb

import (
	"context"
	"testing"
)

func TestRecordIterator_BasicTypes(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	it, err := c.Query(context.Background(),
		"SELECT 1::INTEGER AS a, 'x'::VARCHAR AS b, 3.14::DOUBLE AS c, NULL::VARCHAR AS d")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer it.Close()

	cols := it.Columns()
	if len(cols) != 4 || cols[0].Name != "a" || cols[0].Type != "INTEGER" ||
		cols[1].Type != "VARCHAR" || cols[2].Type != "DOUBLE" || cols[3].Type != "VARCHAR" {
		t.Errorf("unexpected cols: %+v", cols)
	}

	if !it.Next() {
		t.Fatalf("expected one row")
	}
	row := it.Get()
	// DuckDB Go driver returns int32 for INTEGER (not int64).
	if row[0] != int32(1) || row[1] != "x" || row[2] != 3.14 || row[3] != nil {
		t.Errorf("unexpected row: %#v", row)
	}
	if it.Next() {
		t.Errorf("expected only one row")
	}
}

func TestRecordIterator_ArrayType(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	// Use DuckDB list literal syntax [1,2,3] instead of array_value()
	// (go-duckdb v1.7.0 does not return rows for array_value on macOS).
	it, err := c.Query(context.Background(), "SELECT [1, 2, 3] AS a")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer it.Close()
	cols := it.Columns()
	if cols[0].Type != "INTEGER[]" {
		t.Errorf("array type column: got %q want %q", cols[0].Type, "INTEGER[]")
	}
	if !it.Next() {
		t.Fatalf("expected one row")
	}
	row := it.Get()
	arr, ok := row[0].([]any)
	if !ok {
		t.Fatalf("array value: not []any, got %T", row[0])
	}
	if len(arr) != 3 || arr[0] != int32(1) || arr[1] != int32(2) || arr[2] != int32(3) {
		t.Errorf("array values: %#v", arr)
	}
}
