package decisionpoint

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func TestAnalyzeRelationTable(t *testing.T) {
	analysis := rewriteAnalyzer.NewAnalysis(nil)
	table := &ast.Table{Name: ast.QualifiedName{Parts: []string{"s", "t"}}}
	result := AnalyzeRelation(table, nil, nil, analysis)
	if result == nil || result.Type != RelationTypeTable {
		t.Fatalf("expected TABLE, got %v", result)
	}
	if result.TableName != "s.t" {
		t.Fatalf("expected table name s.t, got %s", result.TableName)
	}
}
