package decisionpoint

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
)

func TestAnalyzeSimpleSelect(t *testing.T) {
	sql := "SELECT 1"
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx := &analyzer.SessionContext{Catalog: "test", Schema: "test"}
	// mdl.NewWrenMDL doesn't exist; use the actual constructor.
	wrenMDL := mdl.WrenMDLFromManifest(&dto.Manifest{Catalog: "test", Schema: "test"})
	result := Analyze(stmt, ctx, wrenMDL)
	if len(result) != 1 {
		t.Fatalf("expected 1 query analysis, got %d", len(result))
	}
	if len(result[0].SelectItems) != 1 {
		t.Fatalf("expected 1 select item, got %d", len(result[0].SelectItems))
	}
}
