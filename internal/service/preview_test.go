package service_test

import (
	"context"
	"testing"

	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/service"
)

func TestPreview_RoundtripsSyntheticData(t *testing.T) {
	wrenMDL, _ := mdl.WrenMDLFromJSON(`{"catalog":"wren","schema":"test","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`)
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.NewConfigManager()
	svc := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, cfg)
	res, err := svc.Preview(context.Background(), wrenMDL,
		"SELECT 1 AS a, 'hello' AS b", 100)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(res.Columns) != 2 || res.Columns[0].Name != "a" || res.Columns[1].Name != "b" {
		t.Fatalf("cols: %+v", res.Columns)
	}
	if res.Columns[0].Type != "INTEGER" || res.Columns[1].Type != "VARCHAR" {
		t.Errorf("col types: %+v", res.Columns)
	}
	if len(res.Data) != 1 {
		t.Fatalf("rows: %d", len(res.Data))
	}
	if res.Data[0][0] != int32(1) || res.Data[0][1] != "hello" {
		t.Errorf("rows: %#v", res.Data)
	}
}

func TestPreview_RespectsLimit(t *testing.T) {
	wrenMDL, _ := mdl.WrenMDLFromJSON(`{"catalog":"wren","schema":"test","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`)
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	// Seed a table with 10 rows via init SQL so the query is plain SELECT.
	if err := md.SetInitSQL(context.Background(), "CREATE TABLE nums(n INTEGER); INSERT INTO nums VALUES (0),(1),(2),(3),(4),(5),(6),(7),(8),(9);"); err != nil {
		t.Fatalf("init: %v", err)
	}

	svc := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	res, err := svc.Preview(context.Background(), wrenMDL, "SELECT n FROM nums ORDER BY n", 5)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(res.Data) != 5 {
		t.Errorf("limit=5 expected 5 rows, got %d", len(res.Data))
	}
}
