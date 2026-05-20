package decisionpoint

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func TestAnalyzeFilterAnd(t *testing.T) {
	// Build via parser so we exercise the same LogicalExpression shape as
	// production (N-ary `Terms` with Operator=LogicalAnd, no LogicalBinaryExpression).
	left := &ast.ComparisonExpression{Operator: ast.ComparisonEqual, Left: &ast.Identifier{Value: "a"}, Right: &ast.LongLiteral{Value: 1}}
	right := &ast.ComparisonExpression{Operator: ast.ComparisonEqual, Left: &ast.Identifier{Value: "b"}, Right: &ast.LongLiteral{Value: 2}}
	expr := &ast.LogicalExpression{Operator: ast.LogicalAnd, Terms: []ast.Expression{left, right}}
	scope := rewriteAnalyzer.ScopeBuilderWithParent(nil).Build()
	result := AnalyzeFilter(expr, scope)
	if result == nil || result.Type != FilterTypeAnd {
		t.Fatalf("expected AND, got %v", result)
	}
	if result.Left == nil || result.Left.Type != FilterTypeExpr {
		t.Fatal("expected left leaf")
	}
	if result.Right == nil || result.Right.Type != FilterTypeExpr {
		t.Fatal("expected right leaf")
	}
}
