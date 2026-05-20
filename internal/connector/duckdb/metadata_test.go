package duckdb

import (
	"context"
	"testing"
)

func TestMetadata_InitSQLCreatesTable(t *testing.T) {
	m := NewMetadata()
	t.Cleanup(func() { _ = m.Close() })

	if err := m.SetInitSQL(context.Background(), "CREATE TABLE t(a INTEGER); INSERT INTO t VALUES (1), (2);"); err != nil {
		t.Fatalf("set init: %v", err)
	}

	it, err := m.DirectQuery(context.Background(), "SELECT a FROM t ORDER BY a", nil)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer it.Close()
	var got []int64
	for it.Next() {
		row := it.Get()
		got = append(got, int64(row[0].(int32)))
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("expected [1 2], got %v", got)
	}
}

func TestMetadata_AppendInitSQL(t *testing.T) {
	m := NewMetadata()
	t.Cleanup(func() { _ = m.Close() })

	_ = m.SetInitSQL(context.Background(), "CREATE TABLE t(a INTEGER);")
	if err := m.AppendInitSQL(context.Background(), "INSERT INTO t VALUES (42);"); err != nil {
		t.Fatalf("append: %v", err)
	}
	cols, err := m.DescribeQuery(context.Background(), "SELECT a FROM t", nil)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(cols) != 1 || cols[0].Type != "INTEGER" {
		t.Errorf("cols: %+v", cols)
	}
}
